package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/auth"
	"github.com/feruzabad/debroxy/internal/common/clock"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/common/server"
	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/job/adapters/in/http/client"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/service"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

const (
	origin      = "https://debroxy.example.org"
	sintelHash  = "08ada5a7a6183aae1e09d831df6748d566095a10"
	sintel      = "magnet:?xt=urn:btih:" + sintelHash + "&dn=Sintel"
	webhookPath = "s3cret"
)

// operation adapts a function to any provider or identity handler.
type operation[Req, Res any] func(context.Context, Req) (Res, error)

func (o operation[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	return o(ctx, request)
}

// upstream fakes the provider context behind its operations: one TorBox
// account whose items the test can change.
type upstream struct {
	mu      sync.Mutex
	items   map[string]*providerin.Observation
	order   []string
	deleted []string
	lists   int
	release chan struct{} // when set, creates wait for it
	failing bool          // when set, listings fail
}

func (u *upstream) failListing(failing bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.failing = failing
}

func newUpstream() *upstream {
	return &upstream{items: map[string]*providerin.Observation{}}
}

func (u *upstream) set(change func(items map[string]*providerin.Observation)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	change(u.items)
}

func (u *upstream) listCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lists
}

func (u *upstream) app() providerin.Application {
	return providerin.Application{Operations: providerin.Operations{
		ListProviders: operation[providerin.ListProvidersRequest, []providerin.ProviderSummary](func(context.Context, providerin.ListProvidersRequest) ([]providerin.ProviderSummary, error) {
			return []providerin.ProviderSummary{{ID: "torbox", Kind: "torbox", DisplayName: "TorBox", Enabled: true, Capabilities: []providerin.CapabilityView{
				{Name: "SubmitMagnet", Supported: true, Enabled: true, State: "available"},
				{Name: "SubmitWebURL", Supported: true, Enabled: true, State: "unverified"},
				{Name: "SubmitTorrentFile", Supported: true, Enabled: true, State: "available"},
				{Name: "SubmitNZBFile", Supported: true, Enabled: false},
				{Name: "CachedCheck", Supported: true, Enabled: true, State: "available"},
			}}}, nil
		}),
		HosterCatalogue: operation[providerin.HosterCatalogueRequest, providerin.HosterCatalogue](func(_ context.Context, r providerin.HosterCatalogueRequest) (providerin.HosterCatalogue, error) {
			if r.ProviderID != "torbox" {
				return providerin.HosterCatalogue{}, providerin.ProviderNotFoundError{Provider: r.ProviderID}
			}
			return providerin.HosterCatalogue{Hosters: []providerin.Hoster{{Name: "Mega", Domains: []string{"mega.nz"}, Up: true}}}, nil
		}),
		CheckCached: operation[providerin.CheckCachedRequest, providerin.CheckCachedResult](func(_ context.Context, r providerin.CheckCachedRequest) (providerin.CheckCachedResult, error) {
			items := make([]providerin.CachedItem, 0, len(r.ProviderHashes))
			for _, hash := range r.ProviderHashes {
				items = append(items, providerin.CachedItem{ProviderHash: hash, Cached: hash == sintelHash})
			}
			return providerin.CheckCachedResult{Items: items}, nil
		}),
		CreateRemoteItem: operation[providerin.CreateRemoteItemRequest, providerin.CreateRemoteItemResult](func(ctx context.Context, r providerin.CreateRemoteItemRequest) (providerin.CreateRemoteItemResult, error) {
			u.mu.Lock()
			release := u.release
			u.mu.Unlock()
			if release != nil {
				select {
				case <-release:
				case <-ctx.Done():
					return providerin.CreateRemoteItemResult{}, providerin.AmbiguousCreateError{Provider: "torbox", Cause: "cancelled"}
				}
			}
			u.mu.Lock()
			defer u.mu.Unlock()
			providerItemID := fmt.Sprint(1000 + len(u.order))
			hash := ""
			if i := strings.Index(r.Source.Magnet, "btih:"); i >= 0 {
				hash = strings.ToLower(strings.SplitN(r.Source.Magnet[i+5:], "&", 2)[0])
			}
			observation := &providerin.Observation{ProviderItemID: providerItemID, Kind: providerin.KindTorrent, ProviderHash: hash, State: providerin.StateQueued, Name: "Sintel"}
			u.items[providerItemID] = observation
			u.order = append(u.order, providerItemID)
			return providerin.CreateRemoteItemResult{ProviderItemID: providerItemID, ProviderHash: hash, Observation: *observation}, nil
		}),
		ListRemoteItems: operation[providerin.ListRemoteItemsRequest, providerin.RemoteItemListing](func(context.Context, providerin.ListRemoteItemsRequest) (providerin.RemoteItemListing, error) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.lists++
			if u.failing {
				return providerin.RemoteItemListing{}, providerin.ProviderUnavailableError{Provider: "torbox", Cause: "test"}
			}
			listing := providerin.RemoteItemListing{Complete: true}
			for _, providerItemID := range u.order {
				if item, ok := u.items[providerItemID]; ok {
					listing.Observations = append(listing.Observations, *item)
				}
			}
			return listing, nil
		}),
		DeleteRemoteItem: operation[providerin.DeleteRemoteItemRequest, providerin.DeleteRemoteItemResult](func(_ context.Context, r providerin.DeleteRemoteItemRequest) (providerin.DeleteRemoteItemResult, error) {
			u.mu.Lock()
			defer u.mu.Unlock()
			delete(u.items, r.ProviderItemID)
			u.deleted = append(u.deleted, r.ProviderItemID)
			return providerin.DeleteRemoteItemResult{}, nil
		}),
		TranslateWebhook: operation[providerin.TranslateWebhookRequest, providerin.WebhookSignal](func(_ context.Context, r providerin.TranslateWebhookRequest) (providerin.WebhookSignal, error) {
			if r.ProviderID != "torbox" || r.PathSecret != webhookPath {
				return providerin.WebhookSignal{}, providerin.ErrWebhookRejected
			}
			return providerin.WebhookSignal{RefreshRequested: true}, nil
		}),
		ProbeCapabilities: operation[providerin.ProbeCapabilitiesRequest, []providerin.CapabilityView](func(context.Context, providerin.ProbeCapabilitiesRequest) ([]providerin.CapabilityView, error) {
			return nil, nil
		}),
	}}
}

// identityApp fakes identity's UserDetails: every user exists.
func identityApp() identityin.Application {
	return identityin.Application{Queries: identityin.Queries{
		UserDetails: operation[identityin.UserDetails, identityin.UserSummary](func(_ context.Context, q identityin.UserDetails) (identityin.UserSummary, error) {
			if q.Actor.UserID != q.UserID {
				return identityin.UserSummary{}, commonerrors.NewNotFoundError("no such user", "user-not-found")
			}
			return identityin.UserSummary{ID: q.UserID, Username: q.UserID, Role: "user", Status: "active", PasswordLogin: true,
				CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, nil
		}),
	}}
}

// tokens fakes the identity authenticator: "<user>-token" is that user;
// "root-token" is an admin. It fills the session expiry times like the real
// authenticator so /me can show them (spec §10.4).
// tokens accepts "<user>-token"; a revoked user's token fails Check.
type tokens struct {
	clock   clock.Clock
	revoked *sync.Map
}

func (t tokens) Check(ctx context.Context, credential string) error {
	if user, _ := strings.CutSuffix(credential, "-token"); t.revoked != nil {
		if _, gone := t.revoked.Load(user); gone {
			return commonerrors.NewAuthenticationError("session ended", "session-expired")
		}
	}
	_, err := t.Authenticate(ctx, credential)
	return err
}

func (t tokens) Authenticate(_ context.Context, credential string) (auth.Principal, error) {
	user, ok := strings.CutSuffix(credential, "-token")
	if !ok {
		return auth.Principal{}, commonerrors.NewAuthenticationError("unknown session", "session-expired")
	}
	role := "user"
	if user == "root" {
		role = "admin"
	}
	now := t.clock.Now()
	return auth.Principal{
		UserID: user, Role: role, SessionID: "s-" + user,
		SessionIdleExpiresAt: now.Add(time.Hour), SessionAbsoluteExpiresAt: now.Add(24 * time.Hour),
	}, nil
}

type harness struct {
	t        *testing.T
	upstream *upstream
	clock    *clock.Fixed
	app      in.Application
	server   *httptest.Server
	client   *client.ClientWithResponses
	// revoked users fail the Event Stream's session check.
	revoked *sync.Map
}

func newHarness(t *testing.T, configure ...func(*service.Config)) *harness {
	t.Helper()
	cfg := service.Config{
		DefaultSlotQuota: 3, SubmitRatePerHour: 10, CachedSubmitRatePerHour: 60, SubmitRequestDeadline: 2 * time.Second,
		FollowUpRefreshes: []time.Duration{30 * time.Second}, SubmissionTimeout: 20 * time.Minute, StallThreshold: 30 * time.Minute,
		RefreshMaxAge: 10 * time.Second, RefreshBackoffInitial: 5 * time.Second, RefreshBackoffMax: 2 * time.Minute,
		WebhookRefreshInterval: 50 * time.Millisecond, ForcedRefreshPerUserPerMinute: 2,
		SafetyRefreshActive: 5 * time.Minute, SafetyRefreshIdle: 30 * time.Minute, AbsenceGrace: time.Minute,
		OutboxMaxAttempts: 20, OutboxInitialBackoff: time.Second, OutboxMaxBackoff: time.Hour,
		EventsMaxStreamsPerUser: 2, EventsMaxStreamsTotal: 3,
		Retention: service.RetentionConfig{JobHistory: time.Hour, UnmanagedItemsUnseen: time.Hour, ProcessedOutbox: time.Hour, DeletedItems: time.Hour},
	}
	for _, c := range configure {
		c(&cfg)
	}
	up := newUpstream()
	fixed := clock.NewFixed(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	logger := slog.New(slog.DiscardHandler)
	app, wait, err := service.NewComponentTestApplication(context.Background(), cfg, up.app(), identityApp(), fixed, logger)
	require.NoError(t, err)
	t.Cleanup(wait)

	mux := http.NewServeMux()
	service.RegisterHTTPRoutes(mux, app, service.HTTPConfig{
		UploadLimitBytes: 1024, EventPingInterval: 50 * time.Millisecond, EventSessionCheckInterval: 100 * time.Millisecond,
	}, logger)
	revoked := &sync.Map{}
	handler := server.Chain(mux, server.RequestID(), server.Authenticate(server.AuthenticationConfig{AllowedOrigin: origin}, tokens{clock: fixed, revoked: revoked}, logger))
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	apiClient, err := client.NewClientWithResponses(httpServer.URL)
	require.NoError(t, err)
	return &harness{t: t, upstream: up, clock: fixed, app: app, server: httpServer, client: apiClient, revoked: revoked}
}

func as(user string) client.RequestEditorFn {
	return func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+user+"-token")
		return nil
	}
}

func slugOf(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct{ Slug string }
	require.NoError(t, json.Unmarshal(body, &decoded), string(body))
	return decoded.Slug
}

func magnetBody(jobID, magnet string) client.SubmitJobJSONRequestBody {
	return client.SubmitJobJSONRequestBody{Id: &jobID, ProviderId: "torbox", Source: client.Source{Type: client.Magnet, Magnet: &magnet}}
}

// assertNoInternalFields checks that internal JobView fields never reach a client (S9-F2).
func assertNoInternalFields(t *testing.T, body []byte) {
	t.Helper()
	for _, field := range []string{"fingerprint", "remoteItemId", "providerItemId", "btih"} {
		assert.NotContains(t, strings.ToLower(string(body)), strings.ToLower(field))
	}
}

func TestJobAPI_submit_poll_ready_release(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	jobID := id.New()

	submitted, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, submitted.StatusCode(), string(submitted.Body))
	assert.Equal(t, client.Queued, submitted.JSON201.State)
	assert.True(t, submitted.JSON201.SlotCharged)
	assert.Nil(t, submitted.JSON201.OwnerUsername, "owner name is for admins")
	assertNoInternalFields(t, submitted.Body)

	h.upstream.set(func(items map[string]*providerin.Observation) {
		item := items["1000"]
		item.State, item.PerFileDelivery = providerin.StateReady, true
		item.Files = []providerin.RemoteFile{{FileID: "0", Path: "Sintel/Sintel.mp4", SizeBytes: 129241752}}
		item.Bundle = &providerin.Bundle{Format: providerin.BundleZip}
		item.Telemetry.Progress = 1
	})
	h.clock.Advance(11 * time.Second)

	polled, err := h.client.GetJobWithResponse(ctx, jobID, as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, polled.StatusCode(), string(polled.Body))
	assert.Equal(t, client.Ready, polled.JSON200.State)
	assert.Equal(t, []client.File{{Id: "0", Path: "Sintel/Sintel.mp4", SizeBytes: 129241752}}, polled.JSON200.Files)
	assert.Equal(t, &client.Bundle{Format: client.Zip, RangeSupported: false}, polled.JSON200.Delivery.Bundle)
	assert.False(t, *polled.JSON200.Stale)
	assertNoInternalFields(t, polled.Body)

	list, err := h.client.ListJobsWithResponse(ctx, as("alice"))
	require.NoError(t, err)
	require.Len(t, list.JSON200.Jobs, 1)
	assert.False(t, list.JSON200.Stale)
	assertNoInternalFields(t, list.Body)

	released, err := h.client.ReleaseJobWithResponse(ctx, jobID, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, released.StatusCode())
	require.NoError(t, h.app.Commands.ProcessOutbox.Handle(ctx, in.ProcessOutbox{}))
	assert.Equal(t, []string{"1000"}, h.upstream.deleted)

	history, err := h.client.ListJobHistoryWithResponse(ctx, as("alice"))
	require.NoError(t, err)
	require.Len(t, history.JSON200.Entries, 1)
	assert.Equal(t, "ready", history.JSON200.Entries[0].FinalState)
}

func TestJobAPI_slow_create_answers_submission_pending(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *service.Config) { c.SubmitRequestDeadline = 50 * time.Millisecond })
	release := make(chan struct{})
	h.upstream.release = release
	jobID := id.New()

	pending, err := h.client.SubmitJobWithResponse(context.Background(), magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, pending.StatusCode(), string(pending.Body))
	assert.Equal(t, client.SubmissionPendingSlugSubmissionPending, pending.JSON202.Slug)
	assert.Equal(t, client.Submitting, pending.JSON202.Job.State)

	close(release)
	assert.Eventually(t, func() bool {
		job, err := h.client.GetJobWithResponse(context.Background(), jobID, as("alice"))
		return err == nil && job.JSON200 != nil && job.JSON200.State == client.Queued
	}, 2*time.Second, 10*time.Millisecond)
}

func TestJobAPI_submit_is_idempotent_by_job_id(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	jobID := id.New()

	first, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.StatusCode())
	retry, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, retry.StatusCode())
	assert.Equal(t, first.JSON201.Id, retry.JSON201.Id)

	foreign, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("bob"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, foreign.StatusCode())
	assert.Equal(t, "job-id-conflict", slugOf(t, foreign.Body))

	joined, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), sintel), as("bob"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, joined.StatusCode())
	assert.True(t, joined.JSON201.Shared)
	assert.False(t, joined.JSON201.SlotCharged)
}

func TestJobAPI_duplicate_content_names_the_existing_job(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	existing := id.New()

	first, err := h.client.SubmitJobWithResponse(ctx, magnetBody(existing, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.StatusCode())

	duplicate, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, duplicate.StatusCode(), string(duplicate.Body))
	assert.Equal(t, "job-already-exists", slugOf(t, duplicate.Body))
	assert.Equal(t, "/api/v1/jobs/"+existing, duplicate.HTTPResponse.Header.Get("Location"))
}

func TestJobAPI_refuses_bad_submissions_with_slugs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	big := bytes.Repeat([]byte{'x'}, 1025)
	nzb := []byte("<nzb/>")
	web := "https://unknown.example/file"
	notMagnet := "https://example.org"

	testCases := []struct {
		name   string
		body   client.SubmitJobJSONRequestBody
		status int
		slug   string
	}{
		{"upload_too_large", client.SubmitJobJSONRequestBody{ProviderId: "torbox", Source: client.Source{Type: client.TorrentFile, File: &big}}, http.StatusBadRequest, "upload-too-large"},
		{"not_enabled", client.SubmitJobJSONRequestBody{ProviderId: "torbox", Source: client.Source{Type: client.NzbFile, File: &nzb}}, http.StatusNotImplemented, "capability-not-enabled"},
		{"unsupported_hoster", client.SubmitJobJSONRequestBody{ProviderId: "torbox", Source: client.Source{Type: client.WebUrl, Url: &web}}, http.StatusBadRequest, "unsupported-hoster"},
		{"invalid_source", client.SubmitJobJSONRequestBody{ProviderId: "torbox", Source: client.Source{Type: client.Magnet, Magnet: &notMagnet}}, http.StatusBadRequest, "invalid-source"},
		{"unknown_provider", client.SubmitJobJSONRequestBody{ProviderId: "nope", Source: client.Source{Type: client.Magnet, Magnet: ptr(sintel)}}, http.StatusNotFound, "provider-not-found"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := h.client.SubmitJobWithResponse(ctx, tc.body, as("alice"))
			require.NoError(t, err)
			assert.Equal(t, tc.status, response.StatusCode(), string(response.Body))
			assert.Equal(t, tc.slug, slugOf(t, response.Body))
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestJobAPI_display_names_are_trimmed_and_limited(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	long := strings.Repeat("é", 256)

	submit := client.SubmitJobJSONRequestBody{Id: ptr(id.New()), ProviderId: "torbox", DisplayName: &long,
		Source: client.Source{Type: client.Magnet, Magnet: ptr(sintel)}}
	response, err := h.client.SubmitJobWithResponse(ctx, submit, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode())
	assert.Equal(t, "invalid-display-name", slugOf(t, response.Body))

	jobID := id.New()
	submitted, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, submitted.StatusCode())

	renamed, err := h.client.RenameJobWithResponse(ctx, jobID, client.RenameJobJSONRequestBody{DisplayName: "   "}, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, renamed.StatusCode())
	assert.Equal(t, "invalid-display-name", slugOf(t, renamed.Body))
}

func TestJobAPI_me_shows_the_user_and_the_quota(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), sintel), as("alice"))
	require.NoError(t, err)

	me, err := h.client.GetMeWithResponse(ctx, as("alice"))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, me.StatusCode(), string(me.Body))
	assert.Equal(t, "alice", me.JSON200.User.Username)
	assert.Equal(t, client.QuotaUsage{SlotsUsed: 1, MaxActiveJobs: 3}, me.JSON200.Quota)
}

func TestJobAPI_me_shows_session_expiry_and_submit_rate(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	// A magnet the fake cached check does not know, so the submit is uncached.
	_, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"), as("alice"))
	require.NoError(t, err)

	me, err := h.client.GetMeWithResponse(ctx, as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, me.StatusCode(), string(me.Body))

	now := h.clock.Now()
	assert.Equal(t, client.SessionExpiry{
		IdleExpiresAt:     now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}, me.JSON200.Session)

	resetsAt := now.Add(time.Hour)
	assert.Equal(t, client.SubmitRateClass{Used: 1, PerHour: 10, ResetsAt: &resetsAt}, me.JSON200.SubmitRate.Uncached)
	assert.Equal(t, client.SubmitRateClass{Used: 0, PerHour: 60, ResetsAt: nil}, me.JSON200.SubmitRate.Cached)
}

func TestJobAPI_provider_routes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	providers, err := h.client.ListProvidersWithResponse(ctx, as("alice"))
	require.NoError(t, err)
	require.Len(t, providers.JSON200.Providers, 1)
	assert.Equal(t, "torbox", providers.JSON200.Providers[0].Id)
	assert.Len(t, providers.JSON200.Providers[0].Capabilities, 5)

	hosters, err := h.client.ListHostersWithResponse(ctx, "torbox", as("alice"))
	require.NoError(t, err)
	assert.Equal(t, []client.Hoster{{Name: "Mega", Domains: []string{"mega.nz"}, Up: true}}, hosters.JSON200.Hosters)
	missing, err := h.client.ListHostersWithResponse(ctx, "nope", as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, missing.StatusCode())

	cached, err := h.client.CheckCachedAvailabilityWithResponse(ctx, "torbox",
		client.CheckCachedAvailabilityJSONRequestBody{Fingerprints: []string{"btih:" + sintelHash, "btih:0000000000000000000000000000000000000000", "url:https://x/"}}, as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, cached.StatusCode(), string(cached.Body))
	assert.Equal(t, map[string]bool{"btih:" + sintelHash: true, "btih:0000000000000000000000000000000000000000": false, "url:https://x/": false}, cached.JSON200.Cached)

	forbidden, err := h.client.ProbeProviderCapabilitiesWithResponse(ctx, "torbox", as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, forbidden.StatusCode())
	probed, err := h.client.ProbeProviderCapabilitiesWithResponse(ctx, "torbox", as("root"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, probed.StatusCode())
}

func TestJobAPI_forced_refresh_is_limited(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	for range 2 {
		refreshed, err := h.client.RefreshProviderWithResponse(ctx, "torbox", as("alice"))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, refreshed.StatusCode(), string(refreshed.Body))
		assert.False(t, refreshed.JSON200.Stale)
	}
	limited, err := h.client.RefreshProviderWithResponse(ctx, "torbox", as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, limited.StatusCode())
	assert.Equal(t, "refresh-rate-exceeded", slugOf(t, limited.Body))
	require.NotNil(t, limited.JSON429)
	assert.Equal(t, 60, *limited.JSON429.RetryAfter)
	assert.Equal(t, "60", limited.HTTPResponse.Header.Get("Retry-After"))
}

func TestJobAPI_submit_rate_is_limited_with_retry_hint(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *service.Config) { c.SubmitRatePerHour = 1 })
	ctx := context.Background()

	_, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"), as("alice"))
	require.NoError(t, err)

	limited, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), "magnet:?xt=urn:btih:2222222222222222222222222222222222222222"), as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, limited.StatusCode())
	assert.Equal(t, "submit-rate-exceeded", slugOf(t, limited.Body))
	require.NotNil(t, limited.JSON429)
	assert.Equal(t, 3600, *limited.JSON429.RetryAfter)
	assert.Equal(t, "3600", limited.HTTPResponse.Header.Get("Retry-After"))
}

func TestJobAPI_admin_routes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	jobID := id.New()
	_, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)

	forbidden, err := h.client.ListAllJobsWithResponse(ctx, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, forbidden.StatusCode())

	all, err := h.client.ListAllJobsWithResponse(ctx, as("root"))
	require.NoError(t, err)
	require.Len(t, all.JSON200.Jobs, 1)
	assert.Equal(t, "alice", *all.JSON200.Jobs[0].OwnerUsername)

	quota, err := h.client.SetSlotQuotaWithResponse(ctx, "alice", client.SetSlotQuotaJSONRequestBody{MaxActiveJobs: 1}, as("root"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, quota.StatusCode())
	full, err := h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"), as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, full.StatusCode())
	assert.Equal(t, "slot-quota-exceeded", slugOf(t, full.Body))

	unmanaged, err := h.client.ListUnmanagedItemsWithResponse(ctx, as("root"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, unmanaged.StatusCode())
	outbox, err := h.client.ListParkedOutboxMessagesWithResponse(ctx, as("root"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, outbox.StatusCode())

	released, err := h.client.ReleaseAnyJobWithResponse(ctx, jobID, as("root"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, released.StatusCode())
}

func TestJobAPI_admin_reads_slot_quota(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	forbidden, err := h.client.GetUserSlotQuotaWithResponse(ctx, "alice", as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, forbidden.StatusCode())

	quota, err := h.client.GetUserSlotQuotaWithResponse(ctx, "alice", as("root"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, quota.StatusCode(), string(quota.Body))
	assert.Equal(t, client.SlotQuota{MaxActiveJobs: 3, IsDefault: true, SlotsUsed: 0}, *quota.JSON200)

	_, err = h.client.SetSlotQuotaWithResponse(ctx, "alice", client.SetSlotQuotaJSONRequestBody{MaxActiveJobs: 5}, as("root"))
	require.NoError(t, err)
	_, err = h.client.SubmitJobWithResponse(ctx, magnetBody(id.New(), sintel), as("alice"))
	require.NoError(t, err)

	quota, err = h.client.GetUserSlotQuotaWithResponse(ctx, "alice", as("root"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, quota.StatusCode(), string(quota.Body))
	assert.Equal(t, client.SlotQuota{MaxActiveJobs: 5, IsDefault: false, SlotsUsed: 1}, *quota.JSON200)

	unknown, err := h.client.GetUserSlotQuotaWithResponse(ctx, "nobody", as("root"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, unknown.StatusCode())
	assert.Equal(t, client.SlotQuota{MaxActiveJobs: 3, IsDefault: true, SlotsUsed: 0}, *unknown.JSON200)
}

func TestJobAPI_webhook_asks_for_a_refresh(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	accepted, err := h.client.HandleProviderWebhookWithBodyWithResponse(ctx, "torbox", webhookPath, "application/json", strings.NewReader(`{"type":"download_finished"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, accepted.StatusCode())
	assert.Eventually(t, func() bool { return h.upstream.listCount() == 1 }, time.Second, 5*time.Millisecond)

	rejected, err := h.client.HandleProviderWebhookWithBodyWithResponse(ctx, "torbox", "wrong", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rejected.StatusCode())

	tooLarge, err := h.client.HandleProviderWebhookWithBodyWithResponse(ctx, "torbox", webhookPath, "application/json", bytes.NewReader(make([]byte, 65<<10)))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, tooLarge.StatusCode())
}

func TestJobAPI_protected_routes_reject_anonymous_calls(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	jobID := id.New()

	calls := map[string]func() (*http.Response, error){
		"me":        func() (*http.Response, error) { return h.client.GetMe(ctx) },
		"providers": func() (*http.Response, error) { return h.client.ListProviders(ctx) },
		"hosters":   func() (*http.Response, error) { return h.client.ListHosters(ctx, "torbox") },
		"cached": func() (*http.Response, error) {
			return h.client.CheckCachedAvailability(ctx, "torbox", client.CheckCachedAvailabilityJSONRequestBody{})
		},
		"refresh":   func() (*http.Response, error) { return h.client.RefreshProvider(ctx, "torbox") },
		"list_jobs": func() (*http.Response, error) { return h.client.ListJobs(ctx) },
		"submit":    func() (*http.Response, error) { return h.client.SubmitJob(ctx, magnetBody(jobID, sintel)) },
		"history":   func() (*http.Response, error) { return h.client.ListJobHistory(ctx) },
		"get_job":   func() (*http.Response, error) { return h.client.GetJob(ctx, jobID) },
		"rename_job": func() (*http.Response, error) {
			return h.client.RenameJob(ctx, jobID, client.RenameJobJSONRequestBody{DisplayName: "x"})
		},
		"release_job": func() (*http.Response, error) { return h.client.ReleaseJob(ctx, jobID) },
		"all_jobs":    func() (*http.Response, error) { return h.client.ListAllJobs(ctx) },
		"release_any": func() (*http.Response, error) { return h.client.ReleaseAnyJob(ctx, jobID) },
		"slot_quota": func() (*http.Response, error) {
			return h.client.SetSlotQuota(ctx, "alice", client.SetSlotQuotaJSONRequestBody{})
		},
		"unmanaged":     func() (*http.Response, error) { return h.client.ListUnmanagedItems(ctx) },
		"parked_outbox": func() (*http.Response, error) { return h.client.ListParkedOutboxMessages(ctx) },
		"probe":         func() (*http.Response, error) { return h.client.ProbeProviderCapabilities(ctx, "torbox") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			response, err := call()
			require.NoError(t, err)
			defer func() { require.NoError(t, response.Body.Close()) }()
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		})
	}
}
