package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type probeProviderCapabilitiesHandler struct {
	provider out.ProviderGateway
}

func NewProbeProviderCapabilitiesHandler(provider out.ProviderGateway, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ProbeProviderCapabilitiesHandler {
	if provider == nil {
		panic("nil providerGateway")
	}
	return decorator.ApplyCommandDecorators[in.ProbeProviderCapabilities](probeProviderCapabilitiesHandler{provider: provider}, logger, metricsClient)
}

func (h probeProviderCapabilitiesHandler) Handle(ctx context.Context, cmd in.ProbeProviderCapabilities) error {
	actor, err := toDomainActor(cmd.Actor)
	if err != nil {
		return err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return err
	}
	provider, err := remoteitem.NewProviderID(cmd.ProviderID)
	if err != nil {
		return remoteitem.ProviderNotFoundError{Provider: cmd.ProviderID}
	}
	return h.provider.ProbeCapabilities(ctx, provider)
}
