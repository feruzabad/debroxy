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

type checkCachedOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewCheckCachedOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.CheckCachedOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.CheckCachedRequest, in.CheckCachedResult](
		withNeutralErrors[in.CheckCachedRequest, in.CheckCachedResult](checkCachedOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

func (o checkCachedOperation) Handle(ctx context.Context, request in.CheckCachedRequest) (in.CheckCachedResult, error) {
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, true)
	if err != nil {
		return in.CheckCachedResult{}, err
	}
	now := o.clock.Now()
	if err := a.CheckCapability(account.CapabilityCachedCheck, now); err != nil {
		return in.CheckCachedResult{}, err
	}
	result, callErr := gateway.CheckCached(ctx, request.Kind, request.ProviderHashes)
	if err := recordCapabilityOutcome(ctx, o.accounts, a, account.CapabilityCachedCheck, callErr, now); err != nil {
		return in.CheckCachedResult{}, err
	}
	return result, nil
}
