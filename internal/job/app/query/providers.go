package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type providersHandler struct {
	provider out.ProviderGateway
}

func NewProvidersHandler(provider out.ProviderGateway, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ProvidersHandler {
	if provider == nil {
		panic("nil providerGateway")
	}
	return decorator.ApplyQueryDecorators[in.Providers, []in.ProviderView](providersHandler{provider: provider}, logger, metricsClient)
}

func (h providersHandler) Handle(ctx context.Context, query in.Providers) ([]in.ProviderView, error) {
	if _, err := toDomainActor(query.Actor); err != nil {
		return nil, err
	}
	return h.provider.Providers(ctx)
}
