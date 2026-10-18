package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type providerHostersHandler struct {
	provider out.ProviderGateway
}

func NewProviderHostersHandler(provider out.ProviderGateway, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ProviderHostersHandler {
	if provider == nil {
		panic("nil providerGateway")
	}
	return decorator.ApplyQueryDecorators[in.ProviderHosters, []in.HosterView](providerHostersHandler{provider: provider}, logger, metricsClient)
}

func (h providerHostersHandler) Handle(ctx context.Context, query in.ProviderHosters) ([]in.HosterView, error) {
	if _, err := toDomainActor(query.Actor); err != nil {
		return nil, err
	}
	provider, err := remoteitem.NewProviderID(query.ProviderID)
	if err != nil {
		return nil, remoteitem.ProviderNotFoundError{Provider: query.ProviderID}
	}
	return h.provider.ProviderHosters(ctx, provider)
}
