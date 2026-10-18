package command

import (
	"context"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// RetentionConfig holds the retention periods of spec §5.8.
type RetentionConfig struct {
	JobHistory           time.Duration
	UnmanagedItemsUnseen time.Duration
	ProcessedOutbox      time.Duration
	DeletedItems         time.Duration
}

// submitRateWindow: submit-rate rows count for a rolling hour (DEC-24).
const submitRateWindow = time.Hour

type pruneExpiredRecordsHandler struct {
	retention out.RetentionRepository
	clock     clock.Clock
	config    RetentionConfig
}

func NewPruneExpiredRecordsHandler(retention out.RetentionRepository, clk clock.Clock, config RetentionConfig, logger *slog.Logger, metricsClient decorator.MetricsClient) in.PruneExpiredRecordsHandler {
	if retention == nil {
		panic("nil retentionRepository")
	}
	if clk == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.PruneExpiredRecords](
		pruneExpiredRecordsHandler{retention: retention, clock: clk, config: config}, logger, metricsClient)
}

func (h pruneExpiredRecordsHandler) Handle(ctx context.Context, _ in.PruneExpiredRecords) error {
	now := h.clock.Now()
	return h.retention.PruneExpiredRecords(ctx, out.RetentionCutoffs{
		JobHistoryReleasedBefore: now.Add(-h.config.JobHistory),
		UnmanagedItemsSeenBefore: now.Add(-h.config.UnmanagedItemsUnseen),
		ProcessedOutboxBefore:    now.Add(-h.config.ProcessedOutbox),
		DeletedItemsBefore:       now.Add(-h.config.DeletedItems),
		SubmitRateWindowsBefore:  now.Add(-submitRateWindow),
	})
}
