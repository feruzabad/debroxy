package operation

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type translateWebhookOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	secrets  map[string]string
}

// NewTranslateWebhookOperation takes the webhook path secret of every
// account that accepts webhooks.
func NewTranslateWebhookOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, webhookSecrets map[string]string,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.TranslateWebhookOperation {
	if accounts == nil {
		panic("nil accounts")
	}
	if gateways == nil {
		panic("nil gateways")
	}
	return decorator.ApplyOperationDecorators[in.TranslateWebhookRequest, in.WebhookSignal](
		withNeutralErrors[in.TranslateWebhookRequest, in.WebhookSignal](translateWebhookOperation{accounts: accounts, gateways: gateways, secrets: webhookSecrets}), logger, metricsClient)
}

// Handle authenticates before parsing (IN-5). Unknown provider, webhooks
// not accepted and wrong secret all give the same ErrWebhookRejected, and
// the secret comparison takes constant time (RISK-07).
func (o translateWebhookOperation) Handle(ctx context.Context, request in.TranslateWebhookRequest) (in.WebhookSignal, error) {
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, true)
	if err != nil || !a.WebhooksAccepted() {
		return in.WebhookSignal{}, in.ErrWebhookRejected
	}
	expected, configured := o.secrets[request.ProviderID]
	if !configured || !secretsEqual(expected, request.PathSecret) {
		return in.WebhookSignal{}, in.ErrWebhookRejected
	}
	return gateway.TranslateWebhook(request.Headers, request.Body)
}

// secretsEqual compares digests, so neither content nor length leaks through timing.
func secretsEqual(expected, given string) bool {
	expectedDigest := sha256.Sum256([]byte(expected))
	givenDigest := sha256.Sum256([]byte(given))
	return subtle.ConstantTimeCompare(expectedDigest[:], givenDigest[:]) == 1
}
