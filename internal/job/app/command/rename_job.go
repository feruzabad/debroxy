package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type renameJobHandler struct {
	items out.RemoteItemRepository
}

func NewRenameJobHandler(items out.RemoteItemRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.RenameJobHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	return decorator.ApplyCommandDecorators[in.RenameJob](renameJobHandler{items: items}, logger, metricsClient)
}

func (h renameJobHandler) Handle(ctx context.Context, cmd in.RenameJob) error {
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
			return item, item.Rename(jobID, actor, cmd.DisplayName)
		})
}
