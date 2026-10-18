package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type hosterCatalogueOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewHosterCatalogueOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.HosterCatalogueOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.HosterCatalogueRequest, in.HosterCatalogue](
		withNeutralErrors[in.HosterCatalogueRequest, in.HosterCatalogue](hosterCatalogueOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

// Handle answers only where web submissions are enabled: the catalogue
// exists to validate them (ADR 0003 rule 10).
func (o hosterCatalogueOperation) Handle(ctx context.Context, request in.HosterCatalogueRequest) (in.HosterCatalogue, error) {
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, true)
	if err != nil {
		return in.HosterCatalogue{}, err
	}
	if err := a.CheckCapability(account.CapabilitySubmitWebURL, o.clock.Now()); err != nil {
		return in.HosterCatalogue{}, err
	}
	return gateway.HosterCatalogue(ctx)
}
