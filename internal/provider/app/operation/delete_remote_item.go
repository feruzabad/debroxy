package operation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type deleteRemoteItemOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
}

func NewDeleteRemoteItemOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.DeleteRemoteItemOperation {
	if accounts == nil {
		panic("nil accounts")
	}
	if gateways == nil {
		panic("nil gateways")
	}
	return decorator.ApplyOperationDecorators[in.DeleteRemoteItemRequest, in.DeleteRemoteItemResult](
		withNeutralErrors[in.DeleteRemoteItemRequest, in.DeleteRemoteItemResult](deleteRemoteItemOperation{accounts: accounts, gateways: gateways}), logger, metricsClient)
}

// Handle is verified deletion (ADR 0003 rule 5, ADR 0008): only a success
// answer counts as deleted without a look-up. On any error, including
// not-found, the item is looked up; gone means deleted, anything else is an
// error and the outbox retries.
func (o deleteRemoteItemOperation) Handle(ctx context.Context, request in.DeleteRemoteItemRequest) (in.DeleteRemoteItemResult, error) {
	_, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, false)
	if err != nil {
		return in.DeleteRemoteItemResult{}, err
	}
	deleteErr := gateway.DeleteRemoteItem(ctx, request.Kind, request.ProviderItemID)
	if deleteErr == nil {
		return in.DeleteRemoteItemResult{}, nil
	}
	_, lookupErr := gateway.DescribeRemoteItem(ctx, request.Kind, request.ProviderItemID)
	if errors.Is(lookupErr, in.ErrRemoteItemNotFound) {
		return in.DeleteRemoteItemResult{}, nil
	}
	if lookupErr == nil {
		return in.DeleteRemoteItemResult{}, fmt.Errorf("item still present after delete failed: %w", deleteErr)
	}
	return in.DeleteRemoteItemResult{}, fmt.Errorf("delete failed and the look-up failed too: %w", errors.Join(deleteErr, lookupErr))
}
