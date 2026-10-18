package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

type resumeDeferredSubmissionsHandler struct {
	runner submissionRunner
}

func NewResumeDeferredSubmissionsHandler(runner SubmissionDependencies, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ResumeDeferredSubmissionsHandler {
	return decorator.ApplyCommandDecorators[in.ResumeDeferredSubmissions](
		resumeDeferredSubmissionsHandler{runner: runner.build(logger)}, logger, metricsClient)
}

// Handle re-runs, one after the other, every submitting item with no
// attempt in progress whose blocker is deleted (or pruned) or whose
// NotBefore is zero or past (C6-02). Items released meanwhile are
// deleting and never re-run (C7-03).
func (h resumeDeferredSubmissionsHandler) Handle(ctx context.Context, _ in.ResumeDeferredSubmissions) error {
	items, err := h.runner.items.RemoteItemsInStates(ctx, remoteitem.StateSubmitting)
	if err != nil {
		return err
	}
	now := h.runner.clock.Now()
	var errs []error
	for _, item := range items {
		submission, ok := item.Submission()
		if !ok || submission.HasAttemptInProgress() || now.Before(submission.NotBefore) {
			continue
		}
		if !submission.DeferredBehind.IsZero() {
			blocker, err := h.runner.items.RemoteItem(ctx, submission.DeferredBehind)
			var notFound remoteitem.NotFoundError
			switch {
			case errors.As(err, &notFound):
			case err != nil:
				errs = append(errs, err)
				continue
			case blocker.State() != remoteitem.StateDeleted:
				continue
			}
		}
		var discarded discardedError
		if err := h.runner.execute(ctx, item.ID(), nil); err != nil && !errors.As(err, &discarded) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
