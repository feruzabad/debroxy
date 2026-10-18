package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// outboxBatch bounds the messages taken per run.
const outboxBatch = 50

type processOutboxHandler struct {
	executor deletionExecutor
}

func NewProcessOutboxHandler(deps DeletionDependencies, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ProcessOutboxHandler {
	return decorator.ApplyCommandDecorators[in.ProcessOutbox](processOutboxHandler{executor: deps.build(logger)}, logger, metricsClient)
}

// Handle runs the due messages; a failed message is recorded for its retry
// and does not stop the others.
func (h processOutboxHandler) Handle(ctx context.Context, _ in.ProcessOutbox) error {
	messages, err := h.executor.outbox.DueMessages(ctx, h.executor.clock.Now(), outboxBatch)
	if err != nil {
		return err
	}
	var errs []error
	for _, message := range messages {
		if ctx.Err() != nil {
			break
		}
		if message.Kind != out.MessageKindDeleteRemoteItem {
			errs = append(errs, fmt.Errorf("outbox message %d: unknown kind %q", message.ID, message.Kind))
			continue
		}
		errs = append(errs, h.executor.run(ctx, message))
	}
	return errors.Join(errs...)
}
