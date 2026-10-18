package out

import (
	"context"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// ProviderGateway is what job needs from the provider context (DEP-4). The
// adapter translates provider's neutral errors into job's errors
// (remoteitem.ProviderNotFoundError, CapabilityRefusedError, ...).
type ProviderGateway interface {
	// ProviderIDs lists every configured provider, disabled ones included:
	// their items are still refreshed and deleted (S4-F5).
	ProviderIDs(ctx context.Context) ([]remoteitem.ProviderID, error)
	// Providers and ProviderHosters return the views users see (C9-03).
	Providers(ctx context.Context) ([]in.ProviderView, error)
	ProviderHosters(ctx context.Context, provider remoteitem.ProviderID) ([]in.HosterView, error)
	// ProbeCapabilities resets the provider's unavailable capabilities to
	// unverified (ADR 0003 rule 8).
	ProbeCapabilities(ctx context.Context, provider remoteitem.ProviderID) error
	// CheckSubmittable checks, in the order of ADR 0003 rule 7, that the
	// provider exists and is enabled and that the source type's capability
	// is supported, enabled and not unavailable for the account. It does not
	// call the provider.
	CheckSubmittable(ctx context.Context, provider remoteitem.ProviderID, sourceType remoteitem.SourceType) error
	HosterCatalogue(ctx context.Context, provider remoteitem.ProviderID) ([]remoteitem.Hoster, error)
	// CheckCached reports which provider hashes are cached. A refused
	// CachedCheck capability is a CapabilityRefusedError.
	CheckCached(ctx context.Context, provider remoteitem.ProviderID, kind remoteitem.Kind, providerHashes []string) (map[string]bool, error)
	// CreateRemoteItem sends one create. Errors: CreateNotSentError (nothing
	// sent), AmbiguousCreateError, or a definite failure (any other error).
	CreateRemoteItem(ctx context.Context, provider remoteitem.ProviderID, source remoteitem.Source, cached bool) (CreatedRemoteItem, error)
	// ListRemoteItems lists every item of the usable kinds (spec §5.6 step 1).
	ListRemoteItems(ctx context.Context, provider remoteitem.ProviderID) (RemoteItemListing, error)
	// DeleteRemoteItem deletes the provider item: nil means deleted,
	// confirmed directly or by a look-up; any error means retry (spec §5.7).
	DeleteRemoteItem(ctx context.Context, provider remoteitem.ProviderID, kind remoteitem.Kind, providerItemID remoteitem.ProviderItemID) error
	// TranslateWebhook checks the path secret in constant time and reports
	// whether the webhook asks for a refresh; a rejected webhook is
	// remoteitem.ErrWebhookRejected.
	TranslateWebhook(ctx context.Context, provider remoteitem.ProviderID, pathSecret string, headers map[string][]string, body []byte) (bool, error)
}

// CreatedRemoteItem is a successful create: the provider item ID and the
// item's first observation (state queued when the provider gave none).
type CreatedRemoteItem struct {
	ProviderItemID remoteitem.ProviderItemID
	Observation    remoteitem.Observation
}

// RemoteItemListing: Complete is true only when every page and kind was
// listed without error and every observation could be translated.
type RemoteItemListing struct {
	Observations []remoteitem.Observation
	Complete     bool
}
