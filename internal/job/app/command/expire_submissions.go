package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type expireSubmissionsHandler struct {
	items   out.RemoteItemRepository
	clock   clock.Clock
	timeout time.Duration
}

func NewExpireSubmissionsHandler(items out.RemoteItemRepository, clk clock.Clock, timeout time.Duration, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ExpireSubmissionsHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if clk == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.ExpireSubmissions](
		expireSubmissionsHandler{items: items, clock: clk, timeout: timeout}, logger, metricsClient)
}

// Handle applies ADR 0005 rule 14 to each pending submission in its own
// transaction (D-11).
func (h expireSubmissionsHandler) Handle(ctx context.Context, _ in.ExpireSubmissions) error {
	items, err := h.items.RemoteItemsInStates(ctx, remoteitem.StateSubmitting, remoteitem.StateDeleting)
	if err != nil {
		return err
	}
	var errs []error
	for _, candidate := range items {
		if _, pending := candidate.Submission(); !pending {
			continue
		}
		errs = append(errs, h.items.UpdateRemoteItem(ctx, candidate.ID(), h.expire))
	}
	return errors.Join(errs...)
}

func (h expireSubmissionsHandler) expire(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
	submission, pending := item.Submission()
	if !pending {
		return item, nil
	}
	now := h.clock.Now()
	timedOut := submission.HasAttemptInProgress() && submission.AttemptStartedAt.Before(now.Add(-h.timeout))
	lost := remoteitem.NewFailureReason(remoteitem.FailureSubmissionLost, "")
	switch {
	case item.State() == remoteitem.StateDeleting:
		// Released while submitting: with no create in flight it ends at
		// once (C7-03); with one in flight, after the timeout.
		if !submission.HasAttemptInProgress() || timedOut {
			return item, item.FailSubmission(lost, now)
		}
	case !submission.DeferredBehind.IsZero():
		parked, err := reads.IsDeletionParked(ctx, submission.DeferredBehind)
		if err != nil {
			return nil, err
		}
		if parked {
			return item, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureBlockedByDelete, ""), now)
		}
	case timedOut:
		return item, item.FailSubmission(lost, now)
	}
	return item, nil
}
