package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type jobsOfUserHandler struct {
	jobs out.JobReadModel
}

func NewJobsOfUserHandler(jobs out.JobReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.JobsOfUserHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.JobsOfUser, []in.JobView](jobsOfUserHandler{jobs: jobs}, logger, metricsClient)
}

func (h jobsOfUserHandler) Handle(ctx context.Context, query in.JobsOfUser) ([]in.JobView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	return h.jobs.JobsOfUser(ctx, actor.UserID)
}
