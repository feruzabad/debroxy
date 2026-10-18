package command_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/job/app/command"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

var (
	start   = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	logger  = slog.New(slog.DiscardHandler)
	metrics = decorator.NoOpMetricsClient{}
	alice   = in.Actor{UserID: "alice", Role: "user"}
	bob     = in.Actor{UserID: "bob", Role: "user"}
	carol   = in.Actor{UserID: "carol", Role: "user"}
	admin   = in.Actor{UserID: "root", Role: "admin"}
)

const provider = "torbox"

// createFunc answers one create; call counts from 1.
type createFunc func(ctx context.Context, call int, source remoteitem.Source) (out.CreatedRemoteItem, error)

// fakeGateway is a hand-written ProviderGateway (T-2).
type fakeGateway struct {
	mu            sync.Mutex
	creates       atomic.Int32
	create        createFunc
	submittable   error
	hosters       []remoteitem.Hoster
	cached        map[string]bool
	cachedErr     error
	cachedFlags   []bool
	cachedQueries int
	listing       out.RemoteItemListing
	listErr       error
	lists         int
	deleted       []string
	deleteErr     error
	deleteErrOnce bool
	probes        int
}

func (g *fakeGateway) CheckSubmittable(context.Context, remoteitem.ProviderID, remoteitem.SourceType) error {
	return g.submittable
}

func (g *fakeGateway) HosterCatalogue(context.Context, remoteitem.ProviderID) ([]remoteitem.Hoster, error) {
	return g.hosters, nil
}

func (g *fakeGateway) CheckCached(_ context.Context, _ remoteitem.ProviderID, _ remoteitem.Kind, _ []string) (map[string]bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cachedQueries++
	return g.cached, g.cachedErr
}

func (g *fakeGateway) CreateRemoteItem(ctx context.Context, _ remoteitem.ProviderID, source remoteitem.Source, cached bool) (out.CreatedRemoteItem, error) {
	call := int(g.creates.Add(1))
	g.mu.Lock()
	g.cachedFlags = append(g.cachedFlags, cached)
	create := g.create
	g.mu.Unlock()
	return create(ctx, call, source)
}

func (g *fakeGateway) ProviderIDs(context.Context) ([]remoteitem.ProviderID, error) {
	id, err := remoteitem.NewProviderID(provider)
	return []remoteitem.ProviderID{id}, err
}

func (g *fakeGateway) ListRemoteItems(context.Context, remoteitem.ProviderID) (out.RemoteItemListing, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lists++
	return g.listing, g.listErr
}

func (g *fakeGateway) TranslateWebhook(_ context.Context, _ remoteitem.ProviderID, secret string, _ map[string][]string, _ []byte) (bool, error) {
	if secret != "s3cret" {
		return false, remoteitem.ErrWebhookRejected
	}
	return true, nil
}

func (g *fakeGateway) DeleteRemoteItem(_ context.Context, _ remoteitem.ProviderID, _ remoteitem.Kind, pid remoteitem.ProviderItemID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deleted = append(g.deleted, pid.String())
	if g.deleteErr != nil {
		err := g.deleteErr
		if g.deleteErrOnce {
			g.deleteErr = nil
		}
		return err
	}
	return nil
}

func (g *fakeGateway) deletes() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.deleted...)
}

func (g *fakeGateway) Providers(context.Context) ([]in.ProviderView, error) {
	panic("not used in this test")
}

func (g *fakeGateway) ProviderHosters(context.Context, remoteitem.ProviderID) ([]in.HosterView, error) {
	panic("not used in this test")
}

func (g *fakeGateway) ProbeCapabilities(context.Context, remoteitem.ProviderID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.probes++
	return nil
}

// usernames is a hand-written UserDirectory: every user's name is its ID.
type usernames struct{}

func (usernames) User(_ context.Context, userID string) (in.UserView, error) {
	return in.UserView{ID: userID, Username: userID + "-name"}, nil
}

func (g *fakeGateway) setListing(listing out.RemoteItemListing, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.listing, g.listErr = listing, err
}

func (g *fakeGateway) listCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lists
}

func (g *fakeGateway) setCreate(create createFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.create = create
}

// created answers every create with the provider item ID and state.
func created(t *testing.T, clk clock.Clock, providerItemID string, state remoteitem.State) createFunc {
	return func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		return createdItem(t, clk, providerItemID, state), nil
	}
}

func createdItem(t *testing.T, clk clock.Clock, providerItemID string, state remoteitem.State) out.CreatedRemoteItem {
	t.Helper()
	pid, err := remoteitem.NewProviderItemID(providerItemID)
	require.NoError(t, err)
	observation, err := remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: pid, Kind: remoteitem.KindTorrent, State: state, Name: "content " + providerItemID, ObservedAt: clk.Now(),
	})
	require.NoError(t, err)
	return out.CreatedRemoteItem{ProviderItemID: pid, Observation: observation}
}

func failing(err error) createFunc {
	return func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		return out.CreatedRemoteItem{}, err
	}
}

type fixture struct {
	t          *testing.T
	storage    *memory.MemoryJobStorage
	gateway    *fakeGateway
	changes    *staleRecorder
	clock      *clock.Fixed
	background *command.Background
	config     command.SubmissionConfig
	submit     in.SubmitJobHandler
	execute    in.ExecuteSubmissionHandler
	release    in.ReleaseJobHandler
	rename     in.RenameJobHandler
	setQuota   in.SetSlotQuotaHandler
	refresh    in.RefreshProviderStateHandler
	webhook    in.HandleProviderWebhookHandler
	expire     in.ExpireSubmissionsHandler
	resume     in.ResumeDeferredSubmissionsHandler
	stalls     in.DetectStallsHandler
	followUps  in.RunFollowUpRefreshesHandler
	safety     in.RunSafetyRefreshesHandler
	outbox     in.ProcessOutboxHandler
	prune      in.PruneExpiredRecordsHandler
	refreshCfg command.RefreshConfig
}

func newFixture(t *testing.T, configure ...func(*command.SubmissionConfig)) *fixture {
	return newFixtureWith(t, nil, configure...)
}

func newFixtureWith(t *testing.T, configureRefresh func(*command.RefreshConfig), configure ...func(*command.SubmissionConfig)) *fixture {
	t.Helper()
	clk := clock.NewFixed(start)
	f := &fixture{
		t: t, storage: memory.NewMemoryJobStorage(clk), clock: clk,
		gateway:    &fakeGateway{cached: map[string]bool{}},
		background: command.NewBackground(context.Background()),
		config: command.SubmissionConfig{
			DefaultSlotQuota: 3, SubmitRatePerHour: 10, CachedSubmitRatePerHour: 60,
			RequestDeadline: 5 * time.Second, ExecutionTimeout: 5 * time.Second, FollowUps: command.DefaultFollowUps,
		},
	}
	f.gateway.create = created(t, clk, "1001", remoteitem.StateDownloading)
	for _, c := range configure {
		c(&f.config)
	}
	deps := command.SubmissionDependencies{Items: f.storage, Provider: f.gateway, Clock: clk, Config: f.config}
	f.submit = command.NewSubmitJobHandler(deps, f.storage, usernames{}, f.background, logger, metrics)
	f.execute = command.NewExecuteSubmissionHandler(deps, logger, metrics)
	f.release = command.NewReleaseJobHandler(f.storage, clk, logger, metrics)
	f.rename = command.NewRenameJobHandler(f.storage, logger, metrics)
	f.setQuota = command.NewSetSlotQuotaHandler(f.storage, logger, metrics)
	f.refreshCfg = command.RefreshConfig{
		MaxAge: 10 * time.Second, BackoffInitial: 5 * time.Second, BackoffMax: 2 * time.Minute,
		WebhookWindow: 50 * time.Millisecond, ForcedPerUserPerMinute: 2,
		SafetyActive: 5 * time.Minute, SafetyIdle: 30 * time.Minute, RunTimeout: 5 * time.Second,
	}
	if configureRefresh != nil {
		configureRefresh(&f.refreshCfg)
	}
	f.changes = &staleRecorder{}
	refresher := command.NewRefresher(command.RefreshPipelineDependencies{
		Submission: deps, Unmanaged: f.storage, SyncState: f.storage,
		AbsenceGrace: func(remoteitem.ProviderID) time.Duration { return time.Minute },
		Changes:      f.changes,
	}, f.refreshCfg, f.background, logger)
	f.refresh = command.NewRefreshProviderStateHandler(refresher, logger, metrics)
	f.webhook = command.NewHandleProviderWebhookHandler(f.gateway, refresher, logger, metrics)
	f.expire = command.NewExpireSubmissionsHandler(f.storage, clk, 20*time.Minute, logger, metrics)
	f.resume = command.NewResumeDeferredSubmissionsHandler(deps, logger, metrics)
	f.stalls = command.NewDetectStallsHandler(f.storage, clk, 30*time.Minute, logger, metrics)
	f.followUps = command.NewRunFollowUpRefreshesHandler(f.storage, f.storage, refresher, logger, metrics)
	f.safety = command.NewRunSafetyRefreshesHandler(f.storage, f.gateway, refresher, logger, metrics)
	f.outbox = command.NewProcessOutboxHandler(command.DeletionDependencies{
		Items: f.storage, Outbox: f.storage, Provider: f.gateway, Clock: clk,
		Config: command.OutboxConfig{MaxAttempts: 3, InitialBackoff: time.Second, MaxBackoff: time.Hour},
	}, logger, metrics)
	f.prune = command.NewPruneExpiredRecordsHandler(f.storage, clk, command.RetentionConfig{
		JobHistory: 180 * 24 * time.Hour, UnmanagedItemsUnseen: 7 * 24 * time.Hour, ProcessedOutbox: 30 * 24 * time.Hour, DeletedItems: 30 * 24 * time.Hour,
	}, logger, metrics)
	t.Cleanup(f.background.Wait)
	return f
}

// staleRecorder records the back-off changes the Refresher reports.
type staleRecorder struct {
	mu    sync.Mutex
	stale []out.ProviderStaleChange
}

func (r *staleRecorder) ItemChanged(out.ItemChange) {}

func (r *staleRecorder) ProviderStaleChanged(change out.ProviderStaleChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stale = append(r.stale, change)
}

func (r *staleRecorder) taken() []out.ProviderStaleChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	stale := r.stale
	r.stale = nil
	return stale
}

func magnet(name string) in.Source {
	sum := sha1.Sum([]byte(name))
	return in.Source{Type: "magnet", Magnet: "magnet:?xt=urn:btih:" + hex.EncodeToString(sum[:]) + "&dn=" + name}
}

func btih(name string) string {
	sum := sha1.Sum([]byte(name))
	return hex.EncodeToString(sum[:])
}

// submitAs submits the source as actor and returns the new job's ID.
func (f *fixture) submitAs(actor in.Actor, source in.Source) (string, error) {
	jobID := id.New()
	return jobID, f.submit.Handle(context.Background(), in.SubmitJob{Actor: actor, JobID: jobID, ProviderID: provider, Source: source})
}

func (f *fixture) mustSubmit(actor in.Actor, source in.Source) string {
	f.t.Helper()
	jobID, err := f.submitAs(actor, source)
	require.NoError(f.t, err)
	return jobID
}

func (f *fixture) job(jobID string) in.JobView {
	f.t.Helper()
	view, err := f.storage.JobDetails(context.Background(), jobID)
	require.NoError(f.t, err)
	return view
}

func (f *fixture) item(jobID string) *remoteitem.RemoteItem {
	f.t.Helper()
	return f.itemByID(f.job(jobID).RemoteItemID)
}

func (f *fixture) itemByID(rawID string) *remoteitem.RemoteItem {
	f.t.Helper()
	itemID, err := remoteitem.NewID(rawID)
	require.NoError(f.t, err)
	item, err := f.storage.RemoteItem(context.Background(), itemID)
	require.NoError(f.t, err)
	return item
}

func (f *fixture) history(jobID string) in.HistoryEntry {
	f.t.Helper()
	entry, found, err := f.storage.HistoryEntry(context.Background(), jobID)
	require.NoError(f.t, err)
	require.True(f.t, found, "job %s is not in history", jobID)
	return entry
}

func (f *fixture) update(rawID string, change func(*remoteitem.RemoteItem) error) {
	f.t.Helper()
	itemID, err := remoteitem.NewID(rawID)
	require.NoError(f.t, err)
	require.NoError(f.t, f.storage.UpdateRemoteItem(context.Background(), itemID,
		func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			return item, change(item)
		}))
}

// --- start and join ---------------------------------------------------------

func TestSubmitJob_completes_the_create_within_the_deadline(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	jobID := f.mustSubmit(alice, magnet("sintel"))

	view := f.job(jobID)
	assert.Equal(t, "downloading", view.State)
	assert.Equal(t, "1001", view.ProviderItemID)
	assert.True(t, view.SlotCharged)
	assert.Equal(t, "alice-name", view.OwnerUsername, "username snapshot from the user directory")
	assert.EqualValues(t, 1, f.gateway.creates.Load())
	_, pending := f.item(jobID).Submission()
	assert.False(t, pending, "submission cleared on completion")
}

func TestSubmitJob_joins_a_claimable_item_without_calling_the_provider(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.mustSubmit(alice, magnet("sintel"))

	second := f.mustSubmit(bob, magnet("sintel"))

	assert.EqualValues(t, 1, f.gateway.creates.Load())
	assert.Equal(t, f.job(first).RemoteItemID, f.job(second).RemoteItemID)
	assert.False(t, f.job(second).SlotCharged, "joining costs nothing while the creator holds the charge")
	assert.Equal(t, 2, f.gateway.cachedQueries, "each submit checks the cache before its transaction (A-9)")
}

func TestSubmitJob_joining_a_submitting_item_leaves_the_job_pending(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.RequestDeadline = 50 * time.Millisecond })
	release := make(chan struct{})
	f.gateway.create = func(ctx context.Context, _ int, _ remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return createdItem(t, f.clock, "1001", remoteitem.StateQueued), nil
	}

	first := f.mustSubmit(alice, magnet("slow"))
	assert.Equal(t, "submitting", f.job(first).State, "202 after the deadline")
	second := f.mustSubmit(bob, magnet("slow"))
	assert.Equal(t, "submitting", f.job(second).State)

	close(release)
	f.background.Wait()
	assert.Equal(t, "queued", f.job(first).State)
	assert.Equal(t, "queued", f.job(second).State)
	assert.EqualValues(t, 1, f.gateway.creates.Load())
}

func TestSubmitJob_refuses_a_second_job_of_the_same_user_for_the_same_content(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.mustSubmit(alice, magnet("sintel"))

	_, err := f.submitAs(alice, magnet("sintel"))

	assert.Equal(t, remoteitem.JobAlreadyExistsError{ExistingJobID: mustJobID(t, first)}, err)
}

func mustJobID(t *testing.T, raw string) remoteitem.JobID {
	t.Helper()
	jobID, err := remoteitem.NewJobID(raw)
	require.NoError(t, err)
	return jobID
}

func TestSubmitJob_is_idempotent_by_job_id(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := id.New()
	cmd := in.SubmitJob{Actor: alice, JobID: jobID, ProviderID: provider, Source: magnet("sintel")}
	require.NoError(t, f.submit.Handle(context.Background(), cmd))

	require.NoError(t, f.submit.Handle(context.Background(), cmd), "same owner, same content")
	assert.EqualValues(t, 1, f.gateway.creates.Load())

	other := cmd
	other.Source = magnet("other")
	assert.Equal(t, command.ErrJobIDConflict, f.submit.Handle(context.Background(), other))
	foreign := cmd
	foreign.Actor = bob
	assert.Equal(t, command.ErrJobIDConflict, f.submit.Handle(context.Background(), foreign))
}

func TestSubmitJob_concurrent_submits_of_one_content_call_the_provider_once(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	users := 8
	var wg sync.WaitGroup
	jobIDs := make([]string, users)
	for i := range users {
		wg.Go(func() {
			jobID, err := f.submitAs(in.Actor{UserID: "user" + string(rune('a'+i)), Role: "user"}, magnet("race"))
			assert.NoError(t, err)
			jobIDs[i] = jobID
		})
	}
	wg.Wait()

	assert.EqualValues(t, 1, f.gateway.creates.Load())
	item := f.item(jobIDs[0])
	assert.Len(t, item.Claims(), users)
	assert.False(t, item.ChargeHolder().IsZero())
}

// --- validation and limits --------------------------------------------------

func TestSubmitJob_validation_refuses_before_any_item_exists(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		configure func(*fakeGateway)
		source    in.Source
		want      error
	}{
		{
			name: "capability_refused",
			configure: func(g *fakeGateway) {
				g.submittable = remoteitem.CapabilityRefusedError{Provider: provider, Capability: "SubmitNZBFile", Refusal: remoteitem.CapabilityNotEnabled}
			},
			source: in.Source{Type: "nzb-file", File: []byte("<nzb/>"), FileName: "a.nzb"},
			want:   remoteitem.CapabilityRefusedError{Provider: provider, Capability: "SubmitNZBFile", Refusal: remoteitem.CapabilityNotEnabled},
		},
		{
			name:      "hoster_not_in_catalogue",
			configure: func(g *fakeGateway) { g.hosters = []remoteitem.Hoster{{Domains: []string{"mega.nz"}, Up: true}} },
			source:    in.Source{Type: "web-url", URL: "https://unknown.example/file"},
			want:      remoteitem.UnsupportedHosterError{Provider: provider},
		},
		{
			name:      "hoster_down",
			configure: func(g *fakeGateway) { g.hosters = []remoteitem.Hoster{{Domains: []string{"mega.nz"}, Up: false}} },
			source:    in.Source{Type: "web-url", URL: "https://mega.nz/file/x"},
			want:      remoteitem.UnsupportedHosterError{Provider: provider},
		},
		{
			name:      "invalid_source",
			configure: func(*fakeGateway) {},
			source:    in.Source{Type: "magnet", Magnet: "https://not-a-magnet"},
			want:      remoteitem.InvalidSourceError{Reason: "not a magnet link"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			tc.configure(f.gateway)

			_, err := f.submitAs(alice, tc.source)

			assert.Equal(t, tc.want, err)
			assert.Zero(t, f.gateway.creates.Load())
			jobs, err := f.storage.AllJobs(context.Background())
			require.NoError(t, err)
			assert.Empty(t, jobs)
		})
	}
}

func TestSubmitJob_accepts_a_web_url_of_a_catalogued_hoster(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.hosters = []remoteitem.Hoster{{Domains: []string{"mega.nz"}, Up: true}}

	f.mustSubmit(alice, in.Source{Type: "web-url", URL: "https://www.mega.nz/file/x"})

	assert.EqualValues(t, 1, f.gateway.creates.Load())
	assert.Zero(t, f.gateway.cachedQueries, "web submits skip the cached check (C6-04)")
}

func TestSubmitJob_enforces_the_submit_rate_per_class(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) {
		c.SubmitRatePerHour, c.CachedSubmitRatePerHour, c.DefaultSlotQuota = 2, 3, 100
	})
	f.gateway.create = func(_ context.Context, call int, _ remoteitem.Source) (out.CreatedRemoteItem, error) {
		return createdItem(t, f.clock, "id"+string(rune('0'+call)), remoteitem.StateReady), nil
	}
	f.mustSubmit(alice, magnet("a"))
	f.mustSubmit(alice, magnet("b"))

	_, err := f.submitAs(alice, magnet("c"))
	assert.Equal(t, command.SubmitRateExceededError{Class: "uncached", PerHour: 2, RetryAfterDuration: time.Hour}, err)

	f.gateway.cached = map[string]bool{btih("c"): true}
	f.mustSubmit(alice, magnet("c"))
	assert.Equal(t, []bool{false, false, true}, f.gateway.cachedFlags, "a cached submit uses the cheaper provider class")

	f.mustSubmit(bob, magnet("d"))
	f.clock.Advance(time.Hour)
	f.mustSubmit(alice, magnet("e"))
}

func TestSubmitJob_failed_cached_check_counts_as_uncached(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.SubmitRatePerHour = 1 })
	f.gateway.cached = map[string]bool{btih("a"): true, btih("b"): true}
	f.gateway.cachedErr = remoteitem.ProviderUnavailableError{Provider: provider}
	f.gateway.create = func(_ context.Context, call int, _ remoteitem.Source) (out.CreatedRemoteItem, error) {
		return createdItem(t, f.clock, "id"+string(rune('0'+call)), remoteitem.StateReady), nil
	}
	f.mustSubmit(alice, magnet("a"))

	_, err := f.submitAs(alice, magnet("b"))

	assert.Equal(t, command.SubmitRateExceededError{Class: "uncached", PerHour: 1, RetryAfterDuration: time.Hour}, err)
}

func TestSubmitJob_enforces_the_slot_quota_on_start(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.DefaultSlotQuota = 1 })
	f.mustSubmit(alice, magnet("a"))

	_, err := f.submitAs(alice, magnet("b"))

	assert.Equal(t, slotquota.QuotaExceededError{SlotsUsed: 1, MaxActiveJobs: 1}, err)
}

func TestSubmitJob_checks_the_quota_when_joining_a_spread_charge(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.DefaultSlotQuota = 1 })
	creator := f.mustSubmit(alice, magnet("shared"))
	f.mustSubmit(bob, magnet("shared"))
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: creator}))
	f.mustSubmit(carol, magnet("other"))

	_, err := f.submitAs(carol, magnet("shared"))

	assert.Equal(t, slotquota.QuotaExceededError{SlotsUsed: 1, MaxActiveJobs: 1}, err)
}

// --- outcomes ---------------------------------------------------------------

func TestSubmitJob_definite_failure_with_only_the_creator_within_the_deadline_is_synchronous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.ProviderRejectedError{Provider: provider, Detail: "UNSUPPORTED_SITE"})

	jobID, err := f.submitAs(alice, magnet("bad"))

	assert.Equal(t, remoteitem.ProviderRejectedError{Provider: provider, Detail: "UNSUPPORTED_SITE"}, err)
	entry := f.history(jobID)
	assert.Equal(t, "submitting", entry.FinalState)
	assert.Equal(t, remoteitem.FailureProviderRejected, entry.FailureReason)
	_, err = f.storage.JobDetails(context.Background(), jobID)
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID}, err, "no job is shown")
	usage, err := f.storage.QuotaUsage(context.Background(), "alice", 3)
	require.NoError(t, err)
	assert.Zero(t, usage.SlotsUsed)
}

func TestSubmitJob_definite_failure_after_the_deadline_lands_in_history(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.RequestDeadline = 20 * time.Millisecond })
	release := make(chan struct{})
	f.gateway.create = func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return out.CreatedRemoteItem{}, remoteitem.ProviderRejectedError{Provider: provider, Detail: "bad torrent"}
	}

	jobID, err := f.submitAs(alice, magnet("late"))
	require.NoError(t, err, "202 first")
	close(release)
	f.background.Wait()

	assert.Equal(t, remoteitem.FailureProviderRejected, f.history(jobID).FailureReason)
}

func TestSubmitJob_definite_failure_with_joiners_fails_the_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.RequestDeadline = 20 * time.Millisecond })
	release := make(chan struct{})
	f.gateway.create = func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return out.CreatedRemoteItem{}, remoteitem.ProviderRejectedError{Provider: provider, Detail: "bad torrent"}
	}
	creator := f.mustSubmit(alice, magnet("joined"))
	joiner := f.mustSubmit(bob, magnet("joined"))

	close(release)
	f.background.Wait()

	for _, jobID := range []string{creator, joiner} {
		view := f.job(jobID)
		assert.Equal(t, "failed", view.State)
		assert.Equal(t, remoteitem.FailureProviderRejected, view.FailureReason)
	}
}

func TestSubmitJob_ambiguous_failure_keeps_the_attempt_and_schedules_follow_ups(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.AmbiguousCreateError{Cause: "HTTP 429"})

	jobID, err := f.submitAs(alice, magnet("unknown"))

	require.NoError(t, err)
	assert.Equal(t, "submitting", f.job(jobID).State)
	submission, ok := f.item(jobID).Submission()
	require.True(t, ok)
	assert.True(t, submission.HasAttemptInProgress(), "the create may exist")
	assert.Equal(t, []time.Time{
		start.Add(30 * time.Second), start.Add(2 * time.Minute), start.Add(5 * time.Minute),
		start.Add(10 * time.Minute), start.Add(15 * time.Minute),
	}, submission.FollowUpsDue)

	require.NoError(t, f.execute.Handle(context.Background(), in.ExecuteSubmission{RemoteItemID: f.job(jobID).RemoteItemID}))
	assert.EqualValues(t, 1, f.gateway.creates.Load(), "no re-send while the attempt is in progress")
}

func TestSubmitJob_unavailable_provider_on_create_is_a_definite_failure(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.ProviderUnavailableError{Provider: provider})

	jobID, err := f.submitAs(alice, magnet("creds"))

	assert.Equal(t, remoteitem.ProviderUnavailableError{Provider: provider}, err)
	assert.Equal(t, remoteitem.FailureProviderRejected, f.history(jobID).FailureReason)
}

func TestExecuteSubmission_not_sent_create_waits_for_not_before(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.CreateNotSentError{RetryAfter: 40 * time.Minute})

	jobID := f.mustSubmit(alice, magnet("budget"))

	submission, ok := f.item(jobID).Submission()
	require.True(t, ok)
	assert.Zero(t, submission.Attempts, "the increment is undone")
	assert.False(t, submission.HasAttemptInProgress())
	assert.Equal(t, start.Add(40*time.Minute), submission.NotBefore)

	itemID := f.job(jobID).RemoteItemID
	f.clock.Advance(39 * time.Minute)
	require.NoError(t, f.execute.Handle(context.Background(), in.ExecuteSubmission{RemoteItemID: itemID}))
	assert.EqualValues(t, 1, f.gateway.creates.Load(), "not before NotBefore")

	f.clock.Advance(time.Minute)
	f.gateway.setCreate(created(t, f.clock, "1001", remoteitem.StateQueued))
	require.NoError(t, f.execute.Handle(context.Background(), in.ExecuteSubmission{RemoteItemID: itemID}))
	assert.EqualValues(t, 2, f.gateway.creates.Load())
	assert.Equal(t, "queued", f.job(jobID).State)
}

func TestExecuteSubmission_re_runs_keep_the_cached_class(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.cached = map[string]bool{btih("cached"): true}
	f.gateway.create = failing(remoteitem.CreateNotSentError{RetryAfter: time.Minute})
	jobID := f.mustSubmit(alice, magnet("cached"))
	submission, ok := f.item(jobID).Submission()
	require.True(t, ok)
	assert.True(t, submission.Cached, "the submission stores its cached class")

	f.clock.Advance(time.Minute)
	f.gateway.setCreate(created(t, f.clock, "1001", remoteitem.StateQueued))
	require.NoError(t, f.execute.Handle(context.Background(), in.ExecuteSubmission{RemoteItemID: f.job(jobID).RemoteItemID}))

	assert.Equal(t, []bool{true, true}, f.gateway.cachedFlags, "the re-run after a budget block uses the cheaper class (DEC-49)")
	assert.Equal(t, 1, f.gateway.cachedQueries, "no second cached check")
}

func TestExecuteSubmission_concurrent_runs_send_one_create(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.CreateNotSentError{RetryAfter: time.Minute})
	jobID := f.mustSubmit(alice, magnet("once"))
	itemID := f.job(jobID).RemoteItemID
	f.clock.Advance(time.Minute)
	release := make(chan struct{})
	f.gateway.setCreate(func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return createdItem(t, f.clock, "1001", remoteitem.StateQueued), nil
	})

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			assert.NoError(t, f.execute.Handle(context.Background(), in.ExecuteSubmission{RemoteItemID: itemID}))
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.EqualValues(t, 2, f.gateway.creates.Load(), "the blocked first attempt plus exactly one re-run")
}

func TestSubmitJob_cancelled_request_still_records_the_outcome(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	f.gateway.create = func(callCtx context.Context, _ int, _ remoteitem.Source) (out.CreatedRemoteItem, error) {
		cancel()
		<-release
		if callCtx.Err() != nil {
			return out.CreatedRemoteItem{}, callCtx.Err()
		}
		return createdItem(t, f.clock, "1001", remoteitem.StateQueued), nil
	}
	jobID := id.New()

	err := f.submit.Handle(ctx, in.SubmitJob{Actor: alice, JobID: jobID, ProviderID: provider, Source: magnet("detached")})
	require.NoError(t, err)
	close(release)
	f.background.Wait()

	assert.Equal(t, "queued", f.job(jobID).State, "the provider call does not use the request context")
}

func TestSubmitJob_merges_into_a_claimable_item_holding_the_provider_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.RequestDeadline = 20 * time.Millisecond })
	existing := f.mustSubmit(alice, magnet("x"))
	targetItem := f.job(existing).RemoteItemID

	// S: other fingerprint, same content at the provider. Alice joins S
	// while it is submitting; she already holds a job on the target.
	release := make(chan struct{})
	f.gateway.setCreate(func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return createdItem(t, f.clock, "1001", remoteitem.StateDownloading), nil
	})
	moved := f.mustSubmit(bob, magnet("x-variant"))
	sItem := f.job(moved).RemoteItemID
	refused := f.mustSubmit(alice, magnet("x-variant"))
	close(release)
	f.background.Wait()

	assert.Equal(t, targetItem, f.job(moved).RemoteItemID, "bob's job moved to the tracked item")
	assert.Equal(t, remoteitem.FailureMergedRefused, f.history(refused).FailureReason)
	merged := f.itemByID(sItem)
	assert.Equal(t, remoteitem.StateDeleted, merged.State(), "no claims and no provider item left")
	assert.Equal(t, remoteitem.FailureMergedInto, merged.FailureReason().Code())
	assert.Len(t, f.itemByID(targetItem).Claims(), 2)
}

func TestSubmitJob_defers_behind_a_deleting_item_holding_the_provider_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustSubmit(alice, magnet("x"))
	blocker := f.job(old).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))

	jobID := f.mustSubmit(bob, magnet("x-variant"))

	submission, ok := f.item(jobID).Submission()
	require.True(t, ok)
	assert.Equal(t, blocker, submission.DeferredBehind.String())
	assert.False(t, submission.HasAttemptInProgress())
	assert.Equal(t, "submitting", f.job(jobID).State)
}

func TestSubmitJob_late_deletion_check_restarts_the_create(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustSubmit(alice, magnet("x"))
	blocker := f.job(old).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))

	f.gateway.setCreate(func(_ context.Context, call int, _ remoteitem.Source) (out.CreatedRemoteItem, error) {
		if call == 2 { // first create of S: the old item's deletion completes meanwhile
			f.update(blocker, func(item *remoteitem.RemoteItem) error { return item.ConfirmDeleted(f.clock.Now()) })
			return createdItem(t, f.clock, "1001", remoteitem.StateQueued), nil
		}
		return createdItem(t, f.clock, "2002", remoteitem.StateQueued), nil
	})

	jobID := f.mustSubmit(bob, magnet("x-variant"))

	assert.EqualValues(t, 3, f.gateway.creates.Load())
	assert.Equal(t, "2002", f.job(jobID).ProviderItemID)
}

func TestSubmitJob_released_while_submitting_deletes_the_created_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(c *command.SubmissionConfig) { c.RequestDeadline = 20 * time.Millisecond })
	release := make(chan struct{})
	f.gateway.create = func(context.Context, int, remoteitem.Source) (out.CreatedRemoteItem, error) {
		<-release
		return createdItem(t, f.clock, "1001", remoteitem.StateQueued), nil
	}
	jobID := f.mustSubmit(alice, magnet("gone-early"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))

	close(release)
	f.background.Wait()

	item := f.itemByID(itemID)
	assert.Equal(t, remoteitem.StateDeleting, item.State())
	assert.Equal(t, "1001", item.ProviderItemID().String())
	messages, err := f.storage.DueMessages(context.Background(), f.clock.Now(), 10)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, itemID, messages[0].RemoteItemID.String())
}

// --- other commands ---------------------------------------------------------

func TestReleaseJob_last_release_requests_deletion_and_writes_history(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("done"))

	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))

	assert.Equal(t, "downloading", f.history(jobID).FinalState)
	messages, err := f.storage.DueMessages(context.Background(), f.clock.Now(), 10)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
}

func TestReleaseJob_hides_foreign_jobs_but_lets_admins_release(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("mine"))

	err := f.release.Handle(context.Background(), in.ReleaseJob{Actor: bob, JobID: jobID})
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID}, err)

	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: admin, JobID: jobID}))
}

func TestRenameJob_changes_only_the_owners_display_name(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("named"))

	require.NoError(t, f.rename.Handle(context.Background(), in.RenameJob{Actor: alice, JobID: jobID, DisplayName: "Sintel"}))
	assert.Equal(t, "Sintel", f.job(jobID).DisplayName)

	err := f.rename.Handle(context.Background(), in.RenameJob{Actor: admin, JobID: jobID, DisplayName: "x"})
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID}, err)
}

func TestSetSlotQuota_is_admin_only_and_changes_the_limit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	err := f.setQuota.Handle(context.Background(), in.SetSlotQuota{Actor: alice, UserID: "alice", MaxActiveJobs: 10})
	assert.Equal(t, remoteitem.ErrAdminOnly, err)

	require.NoError(t, f.setQuota.Handle(context.Background(), in.SetSlotQuota{Actor: admin, UserID: "alice", MaxActiveJobs: 0}))
	_, err = f.submitAs(alice, magnet("blocked"))
	assert.Equal(t, slotquota.QuotaExceededError{SlotsUsed: 0, MaxActiveJobs: 0}, err)
}

func TestProbeProviderCapabilities_is_admin_only(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	probe := command.NewProbeProviderCapabilitiesHandler(f.gateway, logger, metrics)

	assert.Equal(t, remoteitem.ErrAdminOnly, probe.Handle(context.Background(), in.ProbeProviderCapabilities{Actor: alice, ProviderID: provider}))
	require.NoError(t, probe.Handle(context.Background(), in.ProbeProviderCapabilities{Actor: admin, ProviderID: provider}))
	assert.Equal(t, 1, f.gateway.probes)
}
