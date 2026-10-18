package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type listProvidersOperation struct {
	accounts out.ProviderAccountRepository
}

func NewListProvidersOperation(accounts out.ProviderAccountRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ListProvidersOperation {
	if accounts == nil {
		panic("nil accounts")
	}
	return decorator.ApplyOperationDecorators[in.ListProvidersRequest, []in.ProviderSummary](
		withNeutralErrors[in.ListProvidersRequest, []in.ProviderSummary](listProvidersOperation{accounts: accounts}), logger, metricsClient)
}

func (o listProvidersOperation) Handle(ctx context.Context, _ in.ListProvidersRequest) ([]in.ProviderSummary, error) {
	accounts, err := o.accounts.ProviderAccounts(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]in.ProviderSummary, 0, len(accounts))
	for _, a := range accounts {
		summaries = append(summaries, in.ProviderSummary{
			ID:           a.ID().String(),
			Kind:         a.Kind().String(),
			DisplayName:  a.DisplayName(),
			Enabled:      a.IsEnabled(),
			Capabilities: capabilityViewsToDTO(a.Capabilities()),
		})
	}
	return summaries, nil
}
