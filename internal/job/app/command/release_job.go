package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type releaseJobHandler struct {
	items out.RemoteItemRepository
	clock clock.Clock
}

func NewReleaseJobHandler(items out.RemoteItemRepository, clk clock.Clock, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ReleaseJobHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if clk == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.ReleaseJob](releaseJobHandler{items: items, clock: clk}, logger, metricsClient)
}

// Handle releases the claim; the repository writes the history entry and,
// for the last claim on an item with a provider item, the outbox message.
func (h releaseJobHandler) Handle(ctx context.Context, cmd in.ReleaseJob) error {
	actor, err := toDomainActor(cmd.Actor)
	if err != nil {
		return err
	}
	jobID, err := remoteitem.NewJobID(cmd.JobID)
	if err != nil {
		return err
	}
	return h.items.UpdateRemoteItemByJob(ctx, jobID, actor,
		func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			return item, item.Release(jobID, actor, h.clock.Now())
		})
}
