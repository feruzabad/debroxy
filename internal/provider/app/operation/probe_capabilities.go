package operation

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type probeCapabilitiesOperation struct {
	accounts out.ProviderAccountRepository
}

func NewProbeCapabilitiesOperation(accounts out.ProviderAccountRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ProbeCapabilitiesOperation {
	if accounts == nil {
		panic("nil accounts")
	}
	return decorator.ApplyOperationDecorators[in.ProbeCapabilitiesRequest, []in.CapabilityView](
		withNeutralErrors[in.ProbeCapabilitiesRequest, []in.CapabilityView](probeCapabilitiesOperation{accounts: accounts}), logger, metricsClient)
}

// Handle makes every unavailable capability unverified, so the next real
// call tests it. Probing by calling the provider is not possible in
// general: for a plan-restricted create, the test is the create itself.
func (o probeCapabilitiesOperation) Handle(ctx context.Context, request in.ProbeCapabilitiesRequest) ([]in.CapabilityView, error) {
	id, err := account.NewID(request.ProviderID)
	if err != nil {
		return nil, account.NotFoundError{ID: request.ProviderID}
	}
	var views []account.CapabilityView
	err = o.accounts.UpdateProviderAccount(ctx, id, func(_ context.Context, a *account.ProviderAccount) error {
		a.AllowCapabilityProbes()
		views = a.Capabilities()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return capabilityViewsToDTO(views), nil
}
