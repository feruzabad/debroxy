package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type listRemoteItemsOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewListRemoteItemsOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.ListRemoteItemsOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.ListRemoteItemsRequest, in.RemoteItemListing](
		withNeutralErrors[in.ListRemoteItemsRequest, in.RemoteItemListing](listRemoteItemsOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

func (o listRemoteItemsOperation) Handle(ctx context.Context, request in.ListRemoteItemsRequest) (in.RemoteItemListing, error) {
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, false)
	if err != nil {
		return in.RemoteItemListing{}, err
	}
	startedAt := o.clock.Now()
	listing, err := gateway.ListRemoteItems(ctx, usableKinds(a, startedAt))
	if err != nil {
		return in.RemoteItemListing{}, err
	}
	for i := range listing.Observations {
		stamp(&listing.Observations[i], startedAt)
	}
	return listing, nil
}
