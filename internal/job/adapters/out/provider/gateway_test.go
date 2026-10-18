package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/provider"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// operation adapts a function to any provider operation.
type operation[Req, Res any] func(context.Context, Req) (Res, error)

func (o operation[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	return o(ctx, request)
}

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func app(list []providerin.ProviderSummary, create operation[providerin.CreateRemoteItemRequest, providerin.CreateRemoteItemResult]) providerin.Application {
	return providerin.Application{Operations: providerin.Operations{
		ListProviders: operation[providerin.ListProvidersRequest, []providerin.ProviderSummary](
			func(context.Context, providerin.ListProvidersRequest) ([]providerin.ProviderSummary, error) {
				return list, nil
			}),
		CreateRemoteItem: create,
		CheckCached: operation[providerin.CheckCachedRequest, providerin.CheckCachedResult](
			func(_ context.Context, r providerin.CheckCachedRequest) (providerin.CheckCachedResult, error) {
				return providerin.CheckCachedResult{Items: []providerin.CachedItem{{ProviderHash: r.ProviderHashes[0], Cached: true}}}, nil
			}),
		HosterCatalogue: operation[providerin.HosterCatalogueRequest, providerin.HosterCatalogue](
			func(context.Context, providerin.HosterCatalogueRequest) (providerin.HosterCatalogue, error) {
				return providerin.HosterCatalogue{Hosters: []providerin.Hoster{{Name: "Mega", Domains: []string{"mega.nz"}, Up: true}}}, nil
			}),
	}}
}

func noCreate(context.Context, providerin.CreateRemoteItemRequest) (providerin.CreateRemoteItemResult, error) {
	panic("not used in this test")
}

func TestCheckSubmittable_applies_the_refusal_order(t *testing.T) {
	t.Parallel()
	summaries := []providerin.ProviderSummary{
		{ID: "off", Enabled: false},
		{ID: "torbox", Enabled: true, Capabilities: []providerin.CapabilityView{
			{Name: "SubmitMagnet", Supported: true, Enabled: true, State: "available"},
			{Name: "SubmitNZBFile", Supported: true, Enabled: false},
			{Name: "SubmitWebURL", Supported: true, Enabled: true, State: "unavailable"},
			{Name: "SubmitTorrentFile", Supported: true, Enabled: true, State: "unverified"},
		}},
	}
	gateway := provider.NewProviderGateway(app(summaries, noCreate), clock.NewFixed(now))
	torbox, off, missing := mustProvider(t, "torbox"), mustProvider(t, "off"), mustProvider(t, "missing")

	testCases := []struct {
		name       string
		provider   remoteitem.ProviderID
		sourceType remoteitem.SourceType
		want       error
	}{
		{"usable", torbox, remoteitem.SourceMagnet, nil},
		{"unverified_is_usable", torbox, remoteitem.SourceTorrentFile, nil},
		{"unknown_provider", missing, remoteitem.SourceMagnet, remoteitem.ProviderNotFoundError{Provider: "missing"}},
		{"disabled_provider", off, remoteitem.SourceMagnet, remoteitem.ProviderDisabledError{Provider: "off"}},
		{"not_enabled", torbox, remoteitem.SourceNZBFile, remoteitem.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitNZBFile", Refusal: remoteitem.CapabilityNotEnabled}},
		{"unavailable_for_plan", torbox, remoteitem.SourceWebURL, remoteitem.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitWebURL", Refusal: remoteitem.CapabilityUnavailableForPlan}},
		{"not_supported", torbox, remoteitem.SourceNZBURL, remoteitem.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitNZBURL", Refusal: remoteitem.CapabilityNotSupported}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, gateway.CheckSubmittable(context.Background(), tc.provider, tc.sourceType))
		})
	}
}

func mustProvider(t *testing.T, raw string) remoteitem.ProviderID {
	t.Helper()
	id, err := remoteitem.NewProviderID(raw)
	require.NoError(t, err)
	return id
}

func TestCreateRemoteItem_translates_the_result(t *testing.T) {
	t.Parallel()
	var sent providerin.CreateRemoteItemRequest
	gateway := provider.NewProviderGateway(app(nil, func(_ context.Context, r providerin.CreateRemoteItemRequest) (providerin.CreateRemoteItemResult, error) {
		sent = r
		return providerin.CreateRemoteItemResult{ProviderItemID: "42", ProviderHash: "abc", Observation: providerin.Observation{
			State: providerin.StateUnknown, Name: "Sintel", Files: []providerin.RemoteFile{{FileID: "0", Path: "Sintel/a.mp4", SizeBytes: 5}},
			Bundle: &providerin.Bundle{Format: providerin.BundleZip}, PerFileDelivery: true,
		}}, nil
	}), clock.NewFixed(now))

	result, err := gateway.CreateRemoteItem(context.Background(), mustProvider(t, "torbox"), remoteitem.NewWebURLSource("https://mega.nz/x", "pw"), true)

	require.NoError(t, err)
	assert.Equal(t, providerin.CreateRemoteItemRequest{
		ProviderID: "torbox", Cached: true, Source: providerin.Source{Type: providerin.SourceWebURL, URL: "https://mega.nz/x", Password: "pw"},
	}, sent)
	assert.Equal(t, "42", result.ProviderItemID.String())
	assert.Equal(t, "42", result.Observation.ProviderItemID().String())
	assert.Equal(t, "abc", result.Observation.ProviderHash())
	assert.False(t, result.Observation.HasKnownState(), "unknown state keeps the item's state")
	assert.Equal(t, remoteitem.KindWeb, result.Observation.Kind(), "kind falls back to the source's")
	assert.Equal(t, now, result.Observation.ObservedAt(), "stamped when the provider left it empty")
}

func TestCreateRemoteItem_translates_errors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{"not_sent", providerin.CreateNotSentError{Provider: "torbox", RetryAfter: time.Minute}, remoteitem.CreateNotSentError{RetryAfter: time.Minute}},
		{"ambiguous", providerin.AmbiguousCreateError{Provider: "torbox", Cause: "HTTP 429"}, remoteitem.AmbiguousCreateError{Cause: "HTTP 429"}},
		{"cancelled", context.Canceled, remoteitem.AmbiguousCreateError{Cause: context.Canceled.Error()}},
		{"rejected", providerin.ProviderRejectedError{Provider: "torbox", Detail: "bad"}, remoteitem.ProviderRejectedError{Provider: "torbox", Detail: "bad"}},
		{"hoster", providerin.UnsupportedHosterError{Provider: "torbox"}, remoteitem.UnsupportedHosterError{Provider: "torbox"}},
		{"unavailable", providerin.ProviderUnavailableError{Provider: "torbox", RetryAfter: time.Second, Cause: "credentials"}, remoteitem.ProviderUnavailableError{Provider: "torbox", RetryAfterDuration: time.Second}},
		{"capability", providerin.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitWebURL", Refusal: providerin.CapabilityUnavailableForPlan},
			remoteitem.CapabilityRefusedError{Provider: "torbox", Capability: "SubmitWebURL", Refusal: remoteitem.CapabilityUnavailableForPlan}},
		{"not_found", providerin.ProviderNotFoundError{Provider: "x"}, remoteitem.ProviderNotFoundError{Provider: "x"}},
		{"disabled", providerin.ProviderDisabledError{Provider: "x"}, remoteitem.ProviderDisabledError{Provider: "x"}},
		{"item_not_found", providerin.ErrRemoteItemNotFound, remoteitem.ErrProviderItemNotFound},
		{"other", errors.New("boom"), errors.New("boom")},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gateway := provider.NewProviderGateway(app(nil, func(context.Context, providerin.CreateRemoteItemRequest) (providerin.CreateRemoteItemResult, error) {
				return providerin.CreateRemoteItemResult{}, tc.err
			}), clock.NewFixed(now))

			_, err := gateway.CreateRemoteItem(context.Background(), mustProvider(t, "torbox"), remoteitem.NewMagnetSource("magnet:?xt=urn:btih:x"), false)

			assert.Equal(t, tc.want, err)
		})
	}
}

func TestCheckCached_and_HosterCatalogue_translate_results(t *testing.T) {
	t.Parallel()
	gateway := provider.NewProviderGateway(app(nil, noCreate), clock.NewFixed(now))

	cached, err := gateway.CheckCached(context.Background(), mustProvider(t, "torbox"), remoteitem.KindTorrent, []string{"abc"})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"abc": true}, cached)

	hosters, err := gateway.HosterCatalogue(context.Background(), mustProvider(t, "torbox"))
	require.NoError(t, err)
	assert.Equal(t, []remoteitem.Hoster{{Domains: []string{"mega.nz"}, Up: true}}, hosters)
}

func TestListRemoteItems_drops_untranslatable_observations_and_marks_the_listing_incomplete(t *testing.T) {
	t.Parallel()
	application := app([]providerin.ProviderSummary{{ID: "torbox"}, {ID: "rd"}}, noCreate)
	application.Operations.ListRemoteItems = operation[providerin.ListRemoteItemsRequest, providerin.RemoteItemListing](
		func(context.Context, providerin.ListRemoteItemsRequest) (providerin.RemoteItemListing, error) {
			return providerin.RemoteItemListing{Complete: true, Observations: []providerin.Observation{
				{ProviderItemID: "1", Kind: providerin.KindTorrent, State: providerin.StateReady, ObservedAt: now},
				{ProviderItemID: "", Kind: providerin.KindTorrent, State: providerin.StateReady, ObservedAt: now},
			}}, nil
		})
	application.Operations.TranslateWebhook = operation[providerin.TranslateWebhookRequest, providerin.WebhookSignal](
		func(_ context.Context, r providerin.TranslateWebhookRequest) (providerin.WebhookSignal, error) {
			if r.PathSecret != "s3cret" {
				return providerin.WebhookSignal{}, providerin.ErrWebhookRejected
			}
			return providerin.WebhookSignal{RefreshRequested: true}, nil
		})
	gateway := provider.NewProviderGateway(application, clock.NewFixed(now))

	listing, err := gateway.ListRemoteItems(context.Background(), mustProvider(t, "torbox"))
	require.NoError(t, err)
	assert.False(t, listing.Complete)
	require.Len(t, listing.Observations, 1)
	assert.Equal(t, remoteitem.StateReady, listing.Observations[0].State())

	refresh, err := gateway.TranslateWebhook(context.Background(), mustProvider(t, "torbox"), "s3cret", nil, []byte("{}"))
	require.NoError(t, err)
	assert.True(t, refresh)
	_, err = gateway.TranslateWebhook(context.Background(), mustProvider(t, "torbox"), "wrong", nil, nil)
	assert.Equal(t, remoteitem.ErrWebhookRejected, err)

	ids, err := gateway.ProviderIDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []remoteitem.ProviderID{mustProvider(t, "torbox"), mustProvider(t, "rd")}, ids)
}
