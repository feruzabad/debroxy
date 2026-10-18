package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type quotaUsageHandler struct {
	jobs             out.JobReadModel
	defaultSlotQuota int
}

func NewQuotaUsageHandler(jobs out.JobReadModel, defaultSlotQuota int, logger *slog.Logger, metricsClient decorator.MetricsClient) in.QuotaUsageHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.QuotaUsage, in.QuotaUsageResult](quotaUsageHandler{jobs: jobs, defaultSlotQuota: defaultSlotQuota}, logger, metricsClient)
}

func (h quotaUsageHandler) Handle(ctx context.Context, query in.QuotaUsage) (in.QuotaUsageResult, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.QuotaUsageResult{}, err
	}
	return h.jobs.QuotaUsage(ctx, actor.UserID, h.defaultSlotQuota)
}
