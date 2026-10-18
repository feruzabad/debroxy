package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type createRemoteItemOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewCreateRemoteItemOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.CreateRemoteItemOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.CreateRemoteItemRequest, in.CreateRemoteItemResult](
		withNeutralErrors[in.CreateRemoteItemRequest, in.CreateRemoteItemResult](createRemoteItemOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

func (o createRemoteItemOperation) Handle(ctx context.Context, request in.CreateRemoteItemRequest) (in.CreateRemoteItemResult, error) {
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, true)
	if err != nil {
		return in.CreateRemoteItemResult{}, err
	}
	capability, err := capabilityForSource(request.Source.Type)
	if err != nil {
		return in.CreateRemoteItemResult{}, err
	}
	startedAt := o.clock.Now()
	if err := a.CheckCapability(capability, startedAt); err != nil {
		return in.CreateRemoteItemResult{}, err
	}
	result, callErr := gateway.CreateRemoteItem(ctx, request.Source, request.Cached)
	if err := recordCapabilityOutcome(ctx, o.accounts, a, capability, callErr, startedAt); err != nil {
		return in.CreateRemoteItemResult{}, err
	}
	stamp(&result.Observation, startedAt)
	return result, nil
}
