package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// submitRateWindow is the rolling window of the submit rate (spec §9.4).
const submitRateWindow = time.Hour

type submitRateHandler struct {
	jobs                    out.JobReadModel
	submitRatePerHour       int
	cachedSubmitRatePerHour int
	clock                   clock.Clock
}

func NewSubmitRateHandler(
	jobs out.JobReadModel, submitRatePerHour, cachedSubmitRatePerHour int, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.SubmitRateHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyQueryDecorators[in.SubmitRate, in.SubmitRateResult](
		submitRateHandler{jobs: jobs, submitRatePerHour: submitRatePerHour, cachedSubmitRatePerHour: cachedSubmitRatePerHour, clock: clock},
		logger, metricsClient)
}

func (h submitRateHandler) Handle(ctx context.Context, query in.SubmitRate) (in.SubmitRateResult, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.SubmitRateResult{}, err
	}
	usage, err := h.jobs.SubmitRateUsage(ctx, actor.UserID, h.clock.Now())
	if err != nil {
		return in.SubmitRateResult{}, err
	}
	return in.SubmitRateResult{
		Uncached: classResult(usage.Uncached, h.submitRatePerHour),
		Cached:   classResult(usage.Cached, h.cachedSubmitRatePerHour),
	}, nil
}

// classResult turns a storage usage into a class result: the reset time is
// when the oldest counted submit leaves the rolling hour, nil when there
// are none.
func classResult(usage in.SubmitRateClassUsage, perHour int) in.SubmitRateClassResult {
	result := in.SubmitRateClassResult{Used: usage.Used, PerHour: perHour}
	if usage.Used > 0 && !usage.Oldest.IsZero() {
		resetsAt := usage.Oldest.Add(submitRateWindow)
		result.ResetsAt = &resetsAt
	}
	return result
}
