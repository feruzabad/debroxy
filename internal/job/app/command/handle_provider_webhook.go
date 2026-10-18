package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type handleProviderWebhookHandler struct {
	provider  out.ProviderGateway
	refresher *Refresher
}

func NewHandleProviderWebhookHandler(provider out.ProviderGateway, refresher *Refresher, logger *slog.Logger, metricsClient decorator.MetricsClient) in.HandleProviderWebhookHandler {
	if provider == nil {
		panic("nil providerGateway")
	}
	if refresher == nil {
		panic("nil refresher")
	}
	return decorator.ApplyCommandDecorators[in.HandleProviderWebhook](
		handleProviderWebhookHandler{provider: provider, refresher: refresher}, logger, metricsClient)
}

// Handle checks the webhook through provider (constant-time secret check)
// and asks for a coalesced refresh; webhooks are hints only (ADR 0009).
func (h handleProviderWebhookHandler) Handle(ctx context.Context, cmd in.HandleProviderWebhook) error {
	provider, err := remoteitem.NewProviderID(cmd.ProviderID)
	if err != nil {
		return remoteitem.ErrWebhookRejected
	}
	refresh, err := h.provider.TranslateWebhook(ctx, provider, cmd.PathSecret, cmd.Headers, cmd.Body)
	if err != nil {
		return err
	}
	if refresh {
		h.refresher.Webhook(provider)
	}
	return nil
}
