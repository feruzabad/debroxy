package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type detectStallsHandler struct {
	items     out.RemoteItemRepository
	clock     clock.Clock
	threshold time.Duration
}

func NewDetectStallsHandler(items out.RemoteItemRepository, clk clock.Clock, threshold time.Duration, logger *slog.Logger, metricsClient decorator.MetricsClient) in.DetectStallsHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if clk == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.DetectStalls](detectStallsHandler{items: items, clock: clk, threshold: threshold}, logger, metricsClient)
}

func (h detectStallsHandler) Handle(ctx context.Context, _ in.DetectStalls) error {
	items, err := h.items.RemoteItemsInStates(ctx, remoteitem.StateResolvingMetadata, remoteitem.StateDownloading)
	if err != nil {
		return err
	}
	now := h.clock.Now()
	var errs []error
	for _, candidate := range items {
		if candidate.Telemetry().Stalled || now.Sub(candidate.LastProgressAt()) < h.threshold {
			continue
		}
		errs = append(errs, h.items.UpdateRemoteItem(ctx, candidate.ID(),
			func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				item.DetectStall(h.clock.Now(), h.threshold)
				return item, nil
			}))
	}
	return errors.Join(errs...)
}
