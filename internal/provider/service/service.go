// Package service is the composition root of the provider context
// (ddd-rules S-1, S-2). The production NewApplication arrives with the
// first provider adapter (Stage 5); it builds one gateway per configured
// account and runs the startup egress check (ADR 0012 rule 6).
package service

import (
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/provider/app/operation"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

// ComponentTestAccount is one account for NewComponentTestApplication.
type ComponentTestAccount struct {
	Definition    account.Definition
	Gateway       out.ProviderAPIGateway
	WebhookSecret string
}

// NewComponentTestApplication wires memory storage and the given gateways
// (fakes or adapters against fixture servers) for component tests (S-2).
func NewComponentTestApplication(accounts []ComponentTestAccount, clk clock.Clock, logger *slog.Logger) (in.Application, error) {
	built := make([]*account.ProviderAccount, 0, len(accounts))
	gateways := out.ProviderAPIGateways{}
	secrets := map[string]string{}
	for _, a := range accounts {
		providerAccount, err := account.NewProviderAccount(a.Definition, nil)
		if err != nil {
			return in.Application{}, err
		}
		built = append(built, providerAccount)
		gateways[a.Definition.ID.String()] = a.Gateway
		if a.WebhookSecret != "" {
			secrets[a.Definition.ID.String()] = a.WebhookSecret
		}
	}
	return newApplication(memory.NewMemoryProviderAccountRepository(built), gateways, secrets, clk, logger, decorator.NoOpMetricsClient{}), nil
}

func newApplication(
	accounts out.ProviderAccountRepository, gateways out.ProviderAPIGateways, webhookSecrets map[string]string,
	clk clock.Clock, logger *slog.Logger, metrics decorator.MetricsClient,
) in.Application {
	return in.Application{
		Operations: in.Operations{
			ListProviders:      operation.NewListProvidersOperation(accounts, logger, metrics),
			HosterCatalogue:    operation.NewHosterCatalogueOperation(accounts, gateways, clk, logger, metrics),
			CreateRemoteItem:   operation.NewCreateRemoteItemOperation(accounts, gateways, clk, logger, metrics),
			ListRemoteItems:    operation.NewListRemoteItemsOperation(accounts, gateways, clk, logger, metrics),
			DescribeRemoteItem: operation.NewDescribeRemoteItemOperation(accounts, gateways, clk, logger, metrics),
			DeleteRemoteItem:   operation.NewDeleteRemoteItemOperation(accounts, gateways, logger, metrics),
			CheckCached:        operation.NewCheckCachedOperation(accounts, gateways, clk, logger, metrics),
			TranslateWebhook:   operation.NewTranslateWebhookOperation(accounts, gateways, webhookSecrets, logger, metrics),
			ProbeCapabilities:  operation.NewProbeCapabilitiesOperation(accounts, logger, metrics),
			OpenDeliveryStream: operation.NewOpenDeliveryStreamOperation(accounts, gateways, clk, logger, metrics),
		},
	}
}
