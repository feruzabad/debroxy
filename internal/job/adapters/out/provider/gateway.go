// Package provider is job's adapter to the provider context (DEP-4,
// OUT-5): it calls provider's operations and translates types and errors
// both ways.
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// ProviderGateway implements out.ProviderGateway over provider's Application.
type ProviderGateway struct {
	app   providerin.Application
	clock clock.Clock
}

var _ out.ProviderGateway = ProviderGateway{}

func NewProviderGateway(app providerin.Application, clk clock.Clock) ProviderGateway {
	if app.Operations.CreateRemoteItem == nil {
		panic("nil provider application")
	}
	if clk == nil {
		panic("nil clock")
	}
	return ProviderGateway{app: app, clock: clk}
}

// capabilityOf names the provider capability a source type needs (spec §6.3).
var capabilityOf = map[remoteitem.SourceType]string{
	remoteitem.SourceMagnet:      "SubmitMagnet",
	remoteitem.SourceTorrentFile: "SubmitTorrentFile",
	remoteitem.SourceNZBFile:     "SubmitNZBFile",
	remoteitem.SourceNZBURL:      "SubmitNZBURL",
	remoteitem.SourceWebURL:      "SubmitWebURL",
}

func (g ProviderGateway) CheckSubmittable(ctx context.Context, provider remoteitem.ProviderID, sourceType remoteitem.SourceType) error {
	summaries, err := g.app.Operations.ListProviders.Handle(ctx, providerin.ListProvidersRequest{})
	if err != nil {
		return translateError(err)
	}
	for _, summary := range summaries {
		if summary.ID != provider.String() {
			continue
		}
		if !summary.Enabled {
			return remoteitem.ProviderDisabledError{Provider: summary.ID}
		}
		name := capabilityOf[sourceType]
		refused := func(refusal remoteitem.CapabilityRefusal) error {
			return remoteitem.CapabilityRefusedError{Provider: summary.ID, Capability: name, Refusal: refusal}
		}
		for _, capability := range summary.Capabilities {
			if capability.Name != name {
				continue
			}
			switch {
			case !capability.Supported:
				return refused(remoteitem.CapabilityNotSupported)
			case !capability.Enabled:
				return refused(remoteitem.CapabilityNotEnabled)
			case capability.State == "unavailable":
				return refused(remoteitem.CapabilityUnavailableForPlan)
			}
			return nil
		}
		return refused(remoteitem.CapabilityNotSupported)
	}
	return remoteitem.ProviderNotFoundError{Provider: provider.String()}
}

func (g ProviderGateway) HosterCatalogue(ctx context.Context, provider remoteitem.ProviderID) ([]remoteitem.Hoster, error) {
	catalogue, err := g.app.Operations.HosterCatalogue.Handle(ctx, providerin.HosterCatalogueRequest{ProviderID: provider.String()})
	if err != nil {
		return nil, translateError(err)
	}
	hosters := make([]remoteitem.Hoster, 0, len(catalogue.Hosters))
	for _, hoster := range catalogue.Hosters {
		hosters = append(hosters, remoteitem.Hoster{Domains: hoster.Domains, Up: hoster.Up})
	}
	return hosters, nil
}

func (g ProviderGateway) CheckCached(ctx context.Context, provider remoteitem.ProviderID, kind remoteitem.Kind, providerHashes []string) (map[string]bool, error) {
	result, err := g.app.Operations.CheckCached.Handle(ctx, providerin.CheckCachedRequest{
		ProviderID: provider.String(), Kind: providerin.Kind(kind.String()), ProviderHashes: providerHashes,
	})
	if err != nil {
		return nil, translateError(err)
	}
	cached := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		cached[item.ProviderHash] = item.Cached
	}
	return cached, nil
}

func (g ProviderGateway) CreateRemoteItem(ctx context.Context, provider remoteitem.ProviderID, source remoteitem.Source, cached bool) (out.CreatedRemoteItem, error) {
	result, err := g.app.Operations.CreateRemoteItem.Handle(ctx, providerin.CreateRemoteItemRequest{
		ProviderID: provider.String(), Source: sourceToProvider(source), Cached: cached,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// The request may have been sent before the context ended.
			return out.CreatedRemoteItem{}, remoteitem.AmbiguousCreateError{Cause: err.Error()}
		}
		return out.CreatedRemoteItem{}, translateError(err)
	}
	providerItemID, err := remoteitem.NewProviderItemID(result.ProviderItemID)
	if err != nil {
		return out.CreatedRemoteItem{}, remoteitem.AmbiguousCreateError{Cause: "the provider answered without an item ID"}
	}
	observed := result.Observation
	observed.ProviderItemID = result.ProviderItemID
	if observed.ProviderHash == "" {
		observed.ProviderHash = result.ProviderHash
	}
	observation, err := g.observationToDomain(observed, source.Type().Kind())
	if err != nil {
		return out.CreatedRemoteItem{}, fmt.Errorf("translate create observation: %w", err)
	}
	return out.CreatedRemoteItem{ProviderItemID: providerItemID, Observation: observation}, nil
}

func sourceToProvider(source remoteitem.Source) providerin.Source {
	return providerin.Source{
		Type: providerin.SourceType(source.Type().String()), Magnet: source.Magnet(), File: source.File(),
		FileName: source.FileName(), URL: source.URL(), Password: source.Password(),
	}
}

// observationToDomain maps a neutral observation. StateUnknown becomes the
// zero state: the item keeps its state (DEC-09). A missing ObservedAt is
// stamped with debroxy's clock.
func (g ProviderGateway) observationToDomain(o providerin.Observation, fallbackKind remoteitem.Kind) (remoteitem.Observation, error) {
	providerItemID, err := remoteitem.NewProviderItemID(o.ProviderItemID)
	if err != nil {
		return remoteitem.Observation{}, err
	}
	kind := fallbackKind
	if o.Kind != "" {
		if kind, err = remoteitem.NewKindFromString(string(o.Kind)); err != nil {
			return remoteitem.Observation{}, err
		}
	}
	var state remoteitem.State
	if o.State != providerin.StateUnknown && o.State != "" {
		if state, err = remoteitem.NewStateFromString(string(o.State)); err != nil {
			return remoteitem.Observation{}, err
		}
	}
	files := make([]remoteitem.RemoteFile, 0, len(o.Files))
	for _, f := range o.Files {
		file, err := remoteitem.NewRemoteFile(f.FileID, f.Path, f.SizeBytes)
		if err != nil {
			return remoteitem.Observation{}, err
		}
		files = append(files, file)
	}
	var bundle *remoteitem.BundleAvailability
	if o.Bundle != nil {
		format, err := remoteitem.NewBundleFormatFromString(string(o.Bundle.Format))
		if err != nil {
			return remoteitem.Observation{}, err
		}
		b := remoteitem.NewBundleAvailability(format, o.Bundle.RangeSupported)
		bundle = &b
	}
	observedAt := o.ObservedAt
	if observedAt.IsZero() {
		observedAt = g.clock.Now()
	}
	return remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: providerItemID, Kind: kind, ProviderHash: o.ProviderHash, State: state,
		Name: o.Name, SizeBytes: o.SizeBytes, Files: files, Bundle: bundle, PerFileDelivery: o.PerFileDelivery,
		ExpiresAt: o.ExpiresAt, FailureDetail: o.FailureReason, ObservedAt: observedAt,
		Telemetry: remoteitem.Telemetry{
			Progress: o.Telemetry.Progress, BytesPerSecond: o.Telemetry.BytesPerSecond,
			ETA: o.Telemetry.ETA, Peers: o.Telemetry.Peers, Stalled: o.Telemetry.Stalled,
		},
	})
}

func (g ProviderGateway) ProviderIDs(ctx context.Context) ([]remoteitem.ProviderID, error) {
	summaries, err := g.app.Operations.ListProviders.Handle(ctx, providerin.ListProvidersRequest{})
	if err != nil {
		return nil, translateError(err)
	}
	ids := make([]remoteitem.ProviderID, 0, len(summaries))
	for _, summary := range summaries {
		id, err := remoteitem.NewProviderID(summary.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ListRemoteItems translates every observation; one that cannot be
// translated is dropped and makes the listing incomplete, so absence
// detection cannot mark its item gone.
func (g ProviderGateway) ListRemoteItems(ctx context.Context, provider remoteitem.ProviderID) (out.RemoteItemListing, error) {
	listing, err := g.app.Operations.ListRemoteItems.Handle(ctx, providerin.ListRemoteItemsRequest{ProviderID: provider.String()})
	if err != nil {
		return out.RemoteItemListing{}, translateError(err)
	}
	result := out.RemoteItemListing{Complete: listing.Complete, Observations: make([]remoteitem.Observation, 0, len(listing.Observations))}
	for _, observed := range listing.Observations {
		observation, err := g.observationToDomain(observed, remoteitem.Kind{})
		if err != nil {
			result.Complete = false
			continue
		}
		result.Observations = append(result.Observations, observation)
	}
	return result, nil
}

func (g ProviderGateway) TranslateWebhook(ctx context.Context, provider remoteitem.ProviderID, pathSecret string, headers map[string][]string, body []byte) (bool, error) {
	signal, err := g.app.Operations.TranslateWebhook.Handle(ctx, providerin.TranslateWebhookRequest{
		ProviderID: provider.String(), PathSecret: pathSecret, Headers: headers, Body: body,
	})
	if err != nil {
		return false, translateError(err)
	}
	return signal.RefreshRequested, nil
}

func (g ProviderGateway) DeleteRemoteItem(ctx context.Context, provider remoteitem.ProviderID, kind remoteitem.Kind, providerItemID remoteitem.ProviderItemID) error {
	_, err := g.app.Operations.DeleteRemoteItem.Handle(ctx, providerin.DeleteRemoteItemRequest{
		ProviderID: provider.String(), Kind: providerin.Kind(kind.String()), ProviderItemID: providerItemID.String(),
	})
	return translateError(err)
}

func (g ProviderGateway) Providers(ctx context.Context) ([]in.ProviderView, error) {
	summaries, err := g.app.Operations.ListProviders.Handle(ctx, providerin.ListProvidersRequest{})
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]in.ProviderView, 0, len(summaries))
	for _, summary := range summaries {
		view := in.ProviderView{ID: summary.ID, Kind: summary.Kind, DisplayName: summary.DisplayName, Enabled: summary.Enabled,
			Capabilities: make([]in.CapabilityView, 0, len(summary.Capabilities))}
		for _, c := range summary.Capabilities {
			view.Capabilities = append(view.Capabilities, in.CapabilityView{Name: c.Name, Supported: c.Supported, Enabled: c.Enabled, State: c.State})
		}
		views = append(views, view)
	}
	return views, nil
}

func (g ProviderGateway) ProviderHosters(ctx context.Context, provider remoteitem.ProviderID) ([]in.HosterView, error) {
	catalogue, err := g.app.Operations.HosterCatalogue.Handle(ctx, providerin.HosterCatalogueRequest{ProviderID: provider.String()})
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]in.HosterView, 0, len(catalogue.Hosters))
	for _, h := range catalogue.Hosters {
		views = append(views, in.HosterView{Name: h.Name, Domains: h.Domains, Up: h.Up, DailyLimit: h.DailyLimit, DailyUsed: h.DailyUsed})
	}
	return views, nil
}

func (g ProviderGateway) ProbeCapabilities(ctx context.Context, provider remoteitem.ProviderID) error {
	_, err := g.app.Operations.ProbeCapabilities.Handle(ctx, providerin.ProbeCapabilitiesRequest{ProviderID: provider.String()})
	return translateError(err)
}
