package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type executeRemoteItemDeletionHandler struct {
	executor deletionExecutor
}

func NewExecuteRemoteItemDeletionHandler(deps DeletionDependencies, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ExecuteRemoteItemDeletionHandler {
	return decorator.ApplyCommandDecorators[in.ExecuteRemoteItemDeletion](
		executeRemoteItemDeletionHandler{executor: deps.build(logger)}, logger, metricsClient)
}

func (h executeRemoteItemDeletionHandler) Handle(ctx context.Context, cmd in.ExecuteRemoteItemDeletion) error {
	id, err := remoteitem.NewID(cmd.RemoteItemID)
	if err != nil {
		return err
	}
	return h.executor.run(ctx, out.OutboxMessage{ID: cmd.MessageID, Kind: out.MessageKindDeleteRemoteItem, RemoteItemID: id, Attempts: cmd.Attempts})
}
