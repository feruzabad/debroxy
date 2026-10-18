package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type setSlotQuotaHandler struct {
	quotas out.SlotQuotaRepository
}

func NewSetSlotQuotaHandler(quotas out.SlotQuotaRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.SetSlotQuotaHandler {
	if quotas == nil {
		panic("nil slotQuotaRepository")
	}
	return decorator.ApplyCommandDecorators[in.SetSlotQuota](setSlotQuotaHandler{quotas: quotas}, logger, metricsClient)
}

// Handle stores the limit. Lowering it below the current usage takes
// nothing away; it only blocks new charges (ADR 0006).
func (h setSlotQuotaHandler) Handle(ctx context.Context, cmd in.SetSlotQuota) error {
	actor, err := toDomainActor(cmd.Actor)
	if err != nil {
		return err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return err
	}
	quota, err := slotquota.NewSlotQuota(cmd.UserID, cmd.MaxActiveJobs)
	if err != nil {
		return err
	}
	return h.quotas.SetSlotQuota(ctx, quota)
}
