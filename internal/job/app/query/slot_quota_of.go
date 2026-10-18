package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type slotQuotaOfHandler struct {
	jobs             out.JobReadModel
	defaultSlotQuota int
}

func NewSlotQuotaOfHandler(jobs out.JobReadModel, defaultSlotQuota int, logger *slog.Logger, metricsClient decorator.MetricsClient) in.SlotQuotaOfHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.SlotQuotaOf, in.SlotQuotaResult](slotQuotaOfHandler{jobs: jobs, defaultSlotQuota: defaultSlotQuota}, logger, metricsClient)
}

func (h slotQuotaOfHandler) Handle(ctx context.Context, query in.SlotQuotaOf) (in.SlotQuotaResult, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.SlotQuotaResult{}, err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return in.SlotQuotaResult{}, err
	}
	return h.jobs.SlotQuotaOf(ctx, query.UserID, h.defaultSlotQuota)
}
