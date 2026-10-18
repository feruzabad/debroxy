// Package out holds the driven ports of the provider context.
package out

import (
	"context"
	"errors"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// ErrPlanRestricted: the provider said the account's plan does not allow
// this (TorBox PLAN_RESTRICTED_FEATURE). Operations turn it into learned
// capability state and account.CapabilityUnavailableForPlanError.
var ErrPlanRestricted = errors.New("the account's plan does not allow this feature")

// ProviderAPIGateway is one Provider Account's view of its provider's HTTP
// API (R-9). Each provider kind has one adapter; the adapter owns the
// account's HTTP client, rate budgets, egress and hoster catalogue cache
// (ADR 0003 rule 11). Results use the neutral ports/in types (ADR 0016);
// errors are the neutral errors of ports/in, ErrPlanRestricted, or
// ProviderUnavailableError.
type ProviderAPIGateway interface {
	// KindCapabilities is the static capability table of the provider kind
	// (ADR 0003 rule 7).
	KindCapabilities() []account.Capability
	// CreateRemoteItem never retries a create; see the ports/in errors for
	// not-sent and ambiguous outcomes.
	CreateRemoteItem(ctx context.Context, source in.Source, cached bool) (in.CreateRemoteItemResult, error)
	// ListRemoteItems lists every item of the given kinds.
	ListRemoteItems(ctx context.Context, kinds []in.Kind) (in.RemoteItemListing, error)
	DescribeRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) (in.Observation, error)
	// DeleteRemoteItem reports any provider answer other than success as an error.
	DeleteRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) error
	CheckCached(ctx context.Context, kind in.Kind, providerHashes []string) (in.CheckCachedResult, error)
	HosterCatalogue(ctx context.Context) (in.HosterCatalogue, error)
	// TranslateWebhook parses an already authenticated webhook body.
	TranslateWebhook(headers map[string][]string, body []byte) (in.WebhookSignal, error)
	// OpenDeliveryStream resolves the target's CDN link (cached at most
	// cdnLinkCacheMax) and opens it through the account's client (ADR 0010).
	OpenDeliveryStream(ctx context.Context, request in.OpenDeliveryStreamRequest) (in.DeliveryStream, error)
	// CheckEgress opens a TCP connection to the API host and one host per
	// known CDN domain through the pinned dialer (ADR 0012 rule 6).
	CheckEgress(ctx context.Context) error
}

// ProviderAPIGateways maps a Provider Account ID to its gateway.
type ProviderAPIGateways map[string]ProviderAPIGateway
