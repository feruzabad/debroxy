package query_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/job/app/query"
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
	admin   = in.Actor{UserID: "root", Role: "admin"}
)

// readyItem stores a ready item of alice's with one file and a zip bundle
// and returns her job's ID.
func readyItem(t *testing.T, storage *memory.MemoryJobStorage, perFile bool) string {
	t.Helper()
	provider, err := remoteitem.NewProviderID("torbox")
	require.NoError(t, err)
	source := remoteitem.NewMagnetSource("magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0")
	fingerprint, hash, err := remoteitem.Identify(source)
	require.NoError(t, err)
	jobID, err := remoteitem.NewJobID(id.New())
	require.NoError(t, err)
	job, err := remoteitem.NewJob(jobID, "alice", "alice", "Debian", start)
	require.NoError(t, err)
	itemID, err := remoteitem.NewID(id.New())
	require.NoError(t, err)
	pid, err := remoteitem.NewProviderItemID("77")
	require.NoError(t, err)
	file, err := remoteitem.NewRemoteFile("3", "debian/netinst.iso", 700)
	require.NoError(t, err)
	bundle := remoteitem.NewBundleAvailability(remoteitem.BundleFormatZip, false)
	observation, err := remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: pid, Kind: remoteitem.KindTorrent, State: remoteitem.StateReady, Files: []remoteitem.RemoteFile{file},
		Bundle: &bundle, PerFileDelivery: perFile, ObservedAt: start,
	})
	require.NoError(t, err)
	require.NoError(t, storage.JoinOrStart(context.Background(), provider, fingerprint,
		func(context.Context, *remoteitem.RemoteItem, out.RemoteItemReads) (out.JoinOrStartResult, error) {
			item, err := remoteitem.StartSubmission(itemID, provider, source, fingerprint, hash, job, false, start)
			if err != nil {
				return out.JoinOrStartResult{}, err
			}
			return out.JoinOrStartResult{Item: item}, item.CompleteSubmission(pid, observation, start)
		}))
	return jobID.String()
}

func TestJobDetails_hides_foreign_jobs_from_users_but_not_admins(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	jobID := readyItem(t, storage, true)
	handler := query.NewJobDetailsHandler(storage, logger, metrics)

	view, err := handler.Handle(context.Background(), in.JobDetails{Actor: alice, JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, "ready", view.State)

	_, err = handler.Handle(context.Background(), in.JobDetails{Actor: bob, JobID: jobID})
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID}, err)

	_, err = handler.Handle(context.Background(), in.JobDetails{Actor: admin, JobID: jobID})
	assert.NoError(t, err)
}

func TestAdminQueries_refuse_users(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	readyItem(t, storage, true)

	_, err := query.NewAllJobsHandler(storage, logger, metrics).Handle(context.Background(), in.AllJobs{Actor: alice})
	assert.Equal(t, remoteitem.ErrAdminOnly, err)
	_, err = query.NewUnmanagedItemsHandler(storage, logger, metrics).Handle(context.Background(), in.UnmanagedItems{Actor: alice})
	assert.Equal(t, remoteitem.ErrAdminOnly, err)
	_, err = query.NewParkedOutboxMessagesHandler(storage, logger, metrics).Handle(context.Background(), in.ParkedOutboxMessages{Actor: alice})
	assert.Equal(t, remoteitem.ErrAdminOnly, err)

	jobs, err := query.NewAllJobsHandler(storage, logger, metrics).Handle(context.Background(), in.AllJobs{Actor: admin})
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
}

func TestQuotaUsage_uses_the_default_without_a_stored_quota(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))

	usage, err := query.NewQuotaUsageHandler(storage, 4, logger, metrics).Handle(context.Background(), in.QuotaUsage{Actor: alice})

	require.NoError(t, err)
	assert.Equal(t, in.QuotaUsageResult{SlotsUsed: 0, MaxActiveJobs: 4}, usage)
}

func TestSlotQuotaOf_reports_the_default_and_is_admin_only(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	handler := query.NewSlotQuotaOfHandler(storage, 4, logger, metrics)

	result, err := handler.Handle(context.Background(), in.SlotQuotaOf{Actor: admin, UserID: "alice"})
	require.NoError(t, err)
	assert.Equal(t, in.SlotQuotaResult{MaxActiveJobs: 4, IsDefault: true, SlotsUsed: 0}, result)

	_, err = handler.Handle(context.Background(), in.SlotQuotaOf{Actor: alice, UserID: "alice"})
	assert.Equal(t, remoteitem.ErrAdminOnly, err)
}

func TestSlotQuotaOf_reports_a_stored_quota(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	quota, err := slotquota.NewSlotQuota("alice", 7)
	require.NoError(t, err)
	require.NoError(t, storage.SetSlotQuota(context.Background(), quota))

	result, err := query.NewSlotQuotaOfHandler(storage, 4, logger, metrics).Handle(context.Background(), in.SlotQuotaOf{Actor: admin, UserID: "alice"})
	require.NoError(t, err)
	assert.Equal(t, in.SlotQuotaResult{MaxActiveJobs: 7, IsDefault: false, SlotsUsed: 0}, result)
}

func TestDeliveryTarget_resolves_files_and_bundles_for_the_owner(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	jobID := readyItem(t, storage, true)
	handler := query.NewDeliveryTargetHandler(storage, logger, metrics)

	file, err := handler.Handle(context.Background(), in.DeliveryTarget{Actor: alice, JobID: jobID, FileID: "3"})
	require.NoError(t, err)
	assert.Equal(t, in.DeliveryTargetResult{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", FileID: "3", Name: "debian/netinst.iso", RangeSupported: true}, file)

	bundle, err := handler.Handle(context.Background(), in.DeliveryTarget{Actor: alice, JobID: jobID, Bundle: true})
	require.NoError(t, err)
	assert.Equal(t, in.DeliveryTargetResult{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", Bundle: true, Name: "Debian", RangeSupported: false}, bundle)

	_, err = handler.Handle(context.Background(), in.DeliveryTarget{Actor: alice, JobID: jobID, FileID: "9"})
	assert.Equal(t, remoteitem.ErrTargetNotAvailable, err)
	_, err = handler.Handle(context.Background(), in.DeliveryTarget{Actor: admin, JobID: jobID, FileID: "3"})
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID}, err, "links are for the owner only")
}

func TestDeliveryTarget_refuses_files_without_per_file_delivery(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	jobID := readyItem(t, storage, false)

	_, err := query.NewDeliveryTargetHandler(storage, logger, metrics).Handle(context.Background(), in.DeliveryTarget{Actor: alice, JobID: jobID, FileID: "3"})

	assert.Equal(t, remoteitem.ErrTargetNotAvailable, err)
}

// cachedGateway is a hand-written ProviderGateway (T-2).
type cachedGateway struct {
	asked []string
}

func (g *cachedGateway) CheckSubmittable(context.Context, remoteitem.ProviderID, remoteitem.SourceType) error {
	panic("not used in this test")
}
func (g *cachedGateway) HosterCatalogue(context.Context, remoteitem.ProviderID) ([]remoteitem.Hoster, error) {
	panic("not used in this test")
}
func (g *cachedGateway) CreateRemoteItem(context.Context, remoteitem.ProviderID, remoteitem.Source, bool) (out.CreatedRemoteItem, error) {
	panic("not used in this test")
}
func (g *cachedGateway) ProviderIDs(context.Context) ([]remoteitem.ProviderID, error) {
	panic("not used in this test")
}
func (g *cachedGateway) ListRemoteItems(context.Context, remoteitem.ProviderID) (out.RemoteItemListing, error) {
	panic("not used in this test")
}
func (g *cachedGateway) TranslateWebhook(context.Context, remoteitem.ProviderID, string, map[string][]string, []byte) (bool, error) {
	panic("not used in this test")
}
func (g *cachedGateway) DeleteRemoteItem(context.Context, remoteitem.ProviderID, remoteitem.Kind, remoteitem.ProviderItemID) error {
	panic("not used in this test")
}
func (g *cachedGateway) Providers(context.Context) ([]in.ProviderView, error) {
	panic("not used in this test")
}
func (g *cachedGateway) ProviderHosters(context.Context, remoteitem.ProviderID) ([]in.HosterView, error) {
	panic("not used in this test")
}
func (g *cachedGateway) ProbeCapabilities(context.Context, remoteitem.ProviderID) error {
	panic("not used in this test")
}
func (g *cachedGateway) CheckCached(_ context.Context, _ remoteitem.ProviderID, _ remoteitem.Kind, hashes []string) (map[string]bool, error) {
	g.asked = hashes
	return map[string]bool{"aaa": true}, nil
}

func TestCachedAvailability_passes_torrent_hashes_through(t *testing.T) {
	t.Parallel()
	gateway := &cachedGateway{}

	result, err := query.NewCachedAvailabilityHandler(gateway, logger, metrics).Handle(context.Background(), in.CachedAvailability{
		Actor: alice, ProviderID: "torbox", Fingerprints: []string{"btih:aaa", "btih:bbb", "url:https://x/"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"aaa", "bbb"}, gateway.asked)
	assert.Equal(t, map[string]bool{"btih:aaa": true, "btih:bbb": false, "url:https://x/": false}, result.Cached)
}
