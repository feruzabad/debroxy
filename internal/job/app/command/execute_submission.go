package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

type executeSubmissionHandler struct {
	runner submissionRunner
}

func NewExecuteSubmissionHandler(runner SubmissionDependencies, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ExecuteSubmissionHandler {
	return decorator.ApplyCommandDecorators[in.ExecuteSubmission](
		executeSubmissionHandler{runner: runner.build(logger)}, logger, metricsClient)
}

// Handle records every provider outcome; a discarded submission is an
// outcome, not a failure of the command.
func (h executeSubmissionHandler) Handle(ctx context.Context, cmd in.ExecuteSubmission) error {
	id, err := remoteitem.NewID(cmd.RemoteItemID)
	if err != nil {
		return err
	}
	err = h.runner.execute(ctx, id, nil)
	var discarded discardedError
	if errors.As(err, &discarded) {
		return nil
	}
	return err
}
