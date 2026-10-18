package operation_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/provider/app/operation"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

var start = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

var (
	logger  = slog.New(slog.DiscardHandler)
	metrics = decorator.NoOpMetricsClient{}
)

// fakeGateway answers with fixed results and records calls. Methods a test
// does not set up panic.
type fakeGateway struct {
	capabilities  []account.Capability
	createErr     error
	createResult  in.CreateRemoteItemResult
	creates       int
	listing       in.RemoteItemListing
	listedKinds   []in.Kind
	deleteErr     error
	describeErr   error
	describes     int
	cachedErr     error
	webhookCalls  int
	openedTargets []in.DeliveryTarget
}

func (g *fakeGateway) KindCapabilities() []account.Capability { return g.capabilities }

func (g *fakeGateway) CreateRemoteItem(context.Context, in.Source, bool) (in.CreateRemoteItemResult, error) {
	g.creates++
	return g.createResult, g.createErr
}

func (g *fakeGateway) ListRemoteItems(_ context.Context, kinds []in.Kind) (in.RemoteItemListing, error) {
	g.listedKinds = kinds
	return g.listing, nil
}

func (g *fakeGateway) DescribeRemoteItem(context.Context, in.Kind, string) (in.Observation, error) {
	g.describes++
	return in.Observation{ProviderItemID: "42"}, g.describeErr
}

func (g *fakeGateway) DeleteRemoteItem(context.Context, in.Kind, string) error { return g.deleteErr }

func (g *fakeGateway) CheckCached(context.Context, in.Kind, []string) (in.CheckCachedResult, error) {
	return in.CheckCachedResult{Items: []in.CachedItem{{ProviderHash: "abc", Cached: true}}}, g.cachedErr
}

func (g *fakeGateway) HosterCatalogue(context.Context) (in.HosterCatalogue, error) {
	panic("not used in this test")
}

func (g *fakeGateway) OpenDeliveryStream(_ context.Context, request in.OpenDeliveryStreamRequest) (in.DeliveryStream, error) {
	g.openedTargets = append(g.openedTargets, request.Target)
	return in.DeliveryStream{Status: 200}, nil
}

func (g *fakeGateway) CheckEgress(context.Context) error { panic("not used in this test") }

func (g *fakeGateway) TranslateWebhook(map[string][]string, []byte) (in.WebhookSignal, error) {
	g.webhookCalls++
	return in.WebhookSignal{RefreshRequested: true}, nil
}

var torboxCapabilities = []account.Capability{
	account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile, account.CapabilitySubmitNZBFile,
	account.CapabilitySubmitNZBURL, account.CapabilitySubmitWebURL, account.CapabilityPerFileDelivery,
	account.CapabilityBundleDelivery, account.CapabilityCachedCheck, account.CapabilityWebhooks,
	account.CapabilityProviderDedup,
}

type fixture struct {
	accounts *memory.MemoryProviderAccountRepository
	gateway  *fakeGateway
	gateways out.ProviderAPIGateways
	clock    *clock.Fixed
}

func newFixture(t *testing.T, enabled bool) *fixture {
	t.Helper()
	id, err := account.NewID("torbox")
	require.NoError(t, err)
	a, err := account.NewProviderAccount(account.Definition{
		ID: id, Kind: account.KindTorBox, DisplayName: "TorBox", Enabled: enabled, WebhooksEnabled: true,
		KindCapabilities: torboxCapabilities,
	}, nil)
	require.NoError(t, err)
	gateway := &fakeGateway{capabilities: torboxCapabilities}
	return &fixture{
		accounts: memory.NewMemoryProviderAccountRepository([]*account.ProviderAccount{a}),
		gateway:  gateway,
		gateways: out.ProviderAPIGateways{"torbox": gateway},
		clock:    clock.NewFixed(start),
	}
}

func (f *fixture) state(t *testing.T, c account.Capability) account.CapabilityState {
	t.Helper()
	id, err := account.NewID("torbox")
	require.NoError(t, err)
	a, err := f.accounts.ProviderAccount(context.Background(), id)
	require.NoError(t, err)
	state, _ := a.CapabilityState(c)
	return state
}

func TestCreateRemoteItem_stamps_observations_with_the_request_start(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	f.gateway.createResult = in.CreateRemoteItemResult{ProviderItemID: "42", Observation: in.Observation{ProviderItemID: "42"}}
	create := operation.NewCreateRemoteItemOperation(f.accounts, f.gateways, f.clock, logger, metrics)

	result, err := create.Handle(context.Background(), in.CreateRemoteItemRequest{ProviderID: "torbox", Source: in.Source{Type: in.SourceMagnet, Magnet: "magnet:?xt=urn:btih:abc"}})

	require.NoError(t, err)
	assert.Equal(t, "42", result.ProviderItemID)
	assert.Equal(t, start, result.Observation.ObservedAt)
}

func TestCreateRemoteItem_capability_checks_come_first(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		enabled bool
		source  in.SourceType
		want    error
	}{
		{"usenet_not_enabled_in_v1", true, in.SourceNZBFile, in.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitNZBFile", Refusal: in.CapabilityNotEnabled}},
		{"provider_disabled", false, in.SourceMagnet, in.ProviderDisabledError{Provider: "torbox"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tc.enabled)
			create := operation.NewCreateRemoteItemOperation(f.accounts, f.gateways, f.clock, logger, metrics)

			_, err := create.Handle(context.Background(), in.CreateRemoteItemRequest{ProviderID: "torbox", Source: in.Source{Type: tc.source}})

			assert.Equal(t, tc.want, err)
			assert.Zero(t, f.gateway.creates, "the provider is never called")
		})
	}
}

func TestCreateRemoteItem_unknown_provider(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	create := operation.NewCreateRemoteItemOperation(f.accounts, f.gateways, f.clock, logger, metrics)

	_, err := create.Handle(context.Background(), in.CreateRemoteItemRequest{ProviderID: "premiumize", Source: in.Source{Type: in.SourceMagnet}})

	assert.Equal(t, in.ProviderNotFoundError{Provider: "premiumize"}, err)
}

func TestCheckCached_learns_plan_restrictions_and_stops_calling(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	f.gateway.cachedErr = out.ErrPlanRestricted
	check := operation.NewCheckCachedOperation(f.accounts, f.gateways, f.clock, logger, metrics)
	request := in.CheckCachedRequest{ProviderID: "torbox", Kind: in.KindTorrent, ProviderHashes: []string{"abc"}}

	_, err := check.Handle(context.Background(), request)
	assert.Equal(t, in.CapabilityRefusedError{Provider: "torbox", Capability: "CachedCheck", Refusal: in.CapabilityUnavailableForPlan}, err)
	assert.Equal(t, account.Unavailable(account.ReasonPlan, start), f.state(t, account.CapabilityCachedCheck))

	f.gateway.cachedErr = nil
	_, err = check.Handle(context.Background(), request)
	assert.Error(t, err, "rejected locally for 24 h")

	f.clock.Advance(24 * time.Hour)
	result, err := check.Handle(context.Background(), request)
	require.NoError(t, err, "one call tests it again")
	assert.True(t, result.Items[0].Cached)
	assert.Equal(t, account.StatusAvailable, f.state(t, account.CapabilityCachedCheck).Status())
}

func TestDeleteRemoteItem_verifies_failures_with_a_look_up(t *testing.T) {
	t.Parallel()
	providerFailure := errors.New("DATABASE_ERROR")

	testCases := []struct {
		name        string
		deleteErr   error
		describeErr error
		wantDeleted bool
		wantLookups int
	}{
		{"success_needs_no_look_up", nil, nil, true, 0},
		{"failure_then_gone_is_deleted", providerFailure, in.ErrRemoteItemNotFound, true, 1},
		{"not_found_then_gone_is_deleted", in.ErrRemoteItemNotFound, in.ErrRemoteItemNotFound, true, 1},
		{"failure_then_still_present_is_retried", providerFailure, nil, false, 1},
		{"failure_then_look_up_fails_is_retried", providerFailure, errors.New("timeout"), false, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, false) // deletion keeps working for disabled accounts
			f.gateway.deleteErr, f.gateway.describeErr = tc.deleteErr, tc.describeErr
			remove := operation.NewDeleteRemoteItemOperation(f.accounts, f.gateways, logger, metrics)

			_, err := remove.Handle(context.Background(), in.DeleteRemoteItemRequest{ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "42"})

			assert.Equal(t, tc.wantDeleted, err == nil, "error: %v", err)
			assert.Equal(t, tc.wantLookups, f.gateway.describes)
		})
	}
}

func TestListRemoteItems_lists_only_usable_kinds(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	f.gateway.listing = in.RemoteItemListing{Observations: []in.Observation{{ProviderItemID: "1"}, {ProviderItemID: "2"}}, Complete: true}
	list := operation.NewListRemoteItemsOperation(f.accounts, f.gateways, f.clock, logger, metrics)

	listing, err := list.Handle(context.Background(), in.ListRemoteItemsRequest{ProviderID: "torbox"})

	require.NoError(t, err)
	assert.Equal(t, []in.Kind{in.KindTorrent, in.KindWeb}, f.gateway.listedKinds, "usenet is not enabled in v1, so its list is never read")
	assert.True(t, listing.Complete)
	for _, observation := range listing.Observations {
		assert.Equal(t, start, observation.ObservedAt)
	}
}

func TestTranslateWebhook_rejects_without_telling_why(t *testing.T) {
	t.Parallel()
	const secret = "0123456789abcdef0123456789abcdef"

	testCases := []struct {
		name       string
		providerID string
		secret     string
		wantSignal bool
	}{
		{"right_secret", "torbox", secret, true},
		{"wrong_secret", "torbox", secret + "x", false},
		{"unknown_provider", "realdebrid", secret, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, true)
			translate := operation.NewTranslateWebhookOperation(f.accounts, f.gateways, map[string]string{"torbox": secret}, logger, metrics)

			signal, err := translate.Handle(context.Background(), in.TranslateWebhookRequest{ProviderID: tc.providerID, PathSecret: tc.secret, Body: []byte(`{}`)})

			if tc.wantSignal {
				require.NoError(t, err)
				assert.True(t, signal.RefreshRequested)
				return
			}
			assert.ErrorIs(t, err, in.ErrWebhookRejected)
			assert.Zero(t, f.gateway.webhookCalls, "the body is not parsed before authentication (IN-5)")
		})
	}
}

func TestListProviders_and_ProbeCapabilities(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	id, err := account.NewID("torbox")
	require.NoError(t, err)
	require.NoError(t, f.accounts.UpdateProviderAccount(context.Background(), id, func(_ context.Context, a *account.ProviderAccount) error {
		a.RecordPlanRestriction(account.CapabilityCachedCheck, start)
		return nil
	}))
	list := operation.NewListProvidersOperation(f.accounts, logger, metrics)
	probe := operation.NewProbeCapabilitiesOperation(f.accounts, logger, metrics)

	providers, err := list.Handle(context.Background(), in.ListProvidersRequest{})
	require.NoError(t, err)
	require.Len(t, providers, 1)
	assert.Contains(t, providers[0].Capabilities, in.CapabilityView{Name: "CachedCheck", Supported: true, Enabled: true, State: "unavailable"})
	assert.Contains(t, providers[0].Capabilities, in.CapabilityView{Name: "SubmitNZBFile", Supported: true, Enabled: false, State: "unverified"})

	views, err := probe.Handle(context.Background(), in.ProbeCapabilitiesRequest{ProviderID: "torbox"})
	require.NoError(t, err)
	assert.Contains(t, views, in.CapabilityView{Name: "CachedCheck", Supported: true, Enabled: true, State: "unverified"})
}

func TestOpenDeliveryStream_checks_the_target_capability(t *testing.T) {
	t.Parallel()
	id, err := account.NewID("torbox")
	require.NoError(t, err)
	a, err := account.NewProviderAccount(account.Definition{
		ID: id, Kind: account.KindTorBox, Enabled: false, KindCapabilities: torboxCapabilities,
		DisabledCapabilities: []account.Capability{account.CapabilityBundleDelivery},
	}, nil)
	require.NoError(t, err)
	gateway := &fakeGateway{capabilities: torboxCapabilities}
	openStream := operation.NewOpenDeliveryStreamOperation(memory.NewMemoryProviderAccountRepository([]*account.ProviderAccount{a}),
		out.ProviderAPIGateways{"torbox": gateway}, clock.NewFixed(start), logger, metrics)
	request := in.OpenDeliveryStreamRequest{ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "1", Method: http.MethodGet}

	request.Target = in.DeliveryTarget{FileID: "0"}
	stream, err := openStream.Handle(context.Background(), request)
	require.NoError(t, err, "downloads keep working for a disabled account")
	assert.Equal(t, 200, stream.Status)

	request.Target = in.DeliveryTarget{Bundle: true}
	_, err = openStream.Handle(context.Background(), request)
	assert.Equal(t, in.CapabilityRefusedError{Provider: "torbox", Capability: "BundleDelivery", Refusal: in.CapabilityNotEnabled}, err)

	for name, bad := range map[string]in.OpenDeliveryStreamRequest{
		"no_target":    {ProviderID: "torbox", Method: http.MethodGet},
		"both_targets": {ProviderID: "torbox", Method: http.MethodGet, Target: in.DeliveryTarget{FileID: "0", Bundle: true}},
		"wrong_method": {ProviderID: "torbox", Method: http.MethodPost, Target: in.DeliveryTarget{FileID: "0"}},
	} {
		_, err := openStream.Handle(context.Background(), bad)
		assert.Error(t, err, name)
	}
	assert.Equal(t, []in.DeliveryTarget{{FileID: "0"}}, gateway.openedTargets)
}
