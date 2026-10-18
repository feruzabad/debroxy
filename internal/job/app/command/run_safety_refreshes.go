package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type runSafetyRefreshesHandler struct {
	items     out.RemoteItemRepository
	provider  out.ProviderGateway
	refresher *Refresher
}

func NewRunSafetyRefreshesHandler(items out.RemoteItemRepository, provider out.ProviderGateway, refresher *Refresher, logger *slog.Logger, metricsClient decorator.MetricsClient) in.RunSafetyRefreshesHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if provider == nil {
		panic("nil providerGateway")
	}
	if refresher == nil {
		panic("nil refresher")
	}
	return decorator.ApplyCommandDecorators[in.RunSafetyRefreshes](
		runSafetyRefreshesHandler{items: items, provider: provider, refresher: refresher}, logger, metricsClient)
}

// Handle refreshes each provider whose last refresh of any cause finished
// longer ago than its safety interval.
func (h runSafetyRefreshesHandler) Handle(ctx context.Context, _ in.RunSafetyRefreshes) error {
	providers, err := h.provider.ProviderIDs(ctx)
	if err != nil {
		return err
	}
	active, err := h.items.RemoteItemsInStates(ctx, remoteitem.StateSubmitting, remoteitem.StateQueued, remoteitem.StateResolvingMetadata,
		remoteitem.StateAwaitingFileSelection, remoteitem.StateDownloading, remoteitem.StateProcessing)
	if err != nil {
		return err
	}
	hasActive := map[remoteitem.ProviderID]bool{}
	for _, item := range active {
		hasActive[item.ProviderID()] = true
	}
	now := h.refresher.clock.Now()
	var errs []error
	for _, provider := range providers {
		interval := h.refresher.config.SafetyIdle
		if hasActive[provider] {
			interval = h.refresher.config.SafetyActive
		}
		if last := h.refresher.LastRun(provider); !last.IsZero() && now.Sub(last) < interval {
			continue
		}
		errs = append(errs, h.refresher.Refresh(ctx, provider, in.RefreshCauseSafety, ""))
	}
	return errors.Join(errs...)
}
