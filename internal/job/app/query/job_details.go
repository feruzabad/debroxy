package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type jobDetailsHandler struct {
	jobs out.JobReadModel
}

func NewJobDetailsHandler(jobs out.JobReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.JobDetailsHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.JobDetails, in.JobView](jobDetailsHandler{jobs: jobs}, logger, metricsClient)
}

func (h jobDetailsHandler) Handle(ctx context.Context, query in.JobDetails) (in.JobView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.JobView{}, err
	}
	view, err := h.jobs.JobDetails(ctx, query.JobID)
	if err != nil {
		return in.JobView{}, err
	}
	if err := visibleJob(actor, view); err != nil {
		return in.JobView{}, err
	}
	return view, nil
}
