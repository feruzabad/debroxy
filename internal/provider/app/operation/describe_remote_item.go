package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type describeRemoteItemOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewDescribeRemoteItemOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.DescribeRemoteItemOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.DescribeRemoteItemRequest, in.Observation](
		withNeutralErrors[in.DescribeRemoteItemRequest, in.Observation](describeRemoteItemOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

func (o describeRemoteItemOperation) Handle(ctx context.Context, request in.DescribeRemoteItemRequest) (in.Observation, error) {
	_, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, false)
	if err != nil {
		return in.Observation{}, err
	}
	startedAt := o.clock.Now()
	observation, err := gateway.DescribeRemoteItem(ctx, request.Kind, request.ProviderItemID)
	if err != nil {
		return in.Observation{}, err
	}
	stamp(&observation, startedAt)
	return observation, nil
}
