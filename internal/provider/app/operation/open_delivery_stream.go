package operation

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

type openDeliveryStreamOperation struct {
	accounts out.ProviderAccountRepository
	gateways out.ProviderAPIGateways
	clock    clock.Clock
}

func NewOpenDeliveryStreamOperation(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.OpenDeliveryStreamOperation {
	mustHave(accounts, gateways, clock)
	return decorator.ApplyOperationDecorators[in.OpenDeliveryStreamRequest, in.DeliveryStream](
		withNeutralErrors[in.OpenDeliveryStreamRequest, in.DeliveryStream](openDeliveryStreamOperation{accounts: accounts, gateways: gateways, clock: clock}), logger, metricsClient)
}

// Handle checks the delivery capability of the target, then lets the
// adapter resolve and open the CDN link. Downloads keep working for a
// disabled account, so users can fetch what they already have.
func (o openDeliveryStreamOperation) Handle(ctx context.Context, request in.OpenDeliveryStreamRequest) (in.DeliveryStream, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return in.DeliveryStream{}, fmt.Errorf("unsupported delivery method %q", request.Method)
	}
	if request.Target.Bundle == (request.Target.FileID != "") {
		return in.DeliveryStream{}, fmt.Errorf("a delivery target is either a file or the bundle")
	}
	a, gateway, err := accountAndGateway(ctx, o.accounts, o.gateways, request.ProviderID, false)
	if err != nil {
		return in.DeliveryStream{}, err
	}
	capability := account.CapabilityPerFileDelivery
	if request.Target.Bundle {
		capability = account.CapabilityBundleDelivery
	}
	if err := a.CheckCapability(capability, o.clock.Now()); err != nil {
		return in.DeliveryStream{}, err
	}
	return gateway.OpenDeliveryStream(ctx, request)
}
