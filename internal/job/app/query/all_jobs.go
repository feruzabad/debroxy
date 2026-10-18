package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type allJobsHandler struct {
	jobs out.JobReadModel
}

func NewAllJobsHandler(jobs out.JobReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.AllJobsHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.AllJobs, []in.JobView](allJobsHandler{jobs: jobs}, logger, metricsClient)
}

func (h allJobsHandler) Handle(ctx context.Context, query in.AllJobs) ([]in.JobView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return nil, err
	}
	return h.jobs.AllJobs(ctx)
}
