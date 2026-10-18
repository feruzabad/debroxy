package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type jobHistoryHandler struct {
	jobs out.JobReadModel
}

func NewJobHistoryHandler(jobs out.JobReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.JobHistoryHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.JobHistory, []in.HistoryEntry](jobHistoryHandler{jobs: jobs}, logger, metricsClient)
}

func (h jobHistoryHandler) Handle(ctx context.Context, query in.JobHistory) ([]in.HistoryEntry, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	return h.jobs.JobHistory(ctx, actor.UserID)
}
