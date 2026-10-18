package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type runFollowUpRefreshesHandler struct {
	items     out.RemoteItemRepository
	syncState out.SyncStateRepository
	refresher *Refresher
}

func NewRunFollowUpRefreshesHandler(items out.RemoteItemRepository, syncState out.SyncStateRepository, refresher *Refresher, logger *slog.Logger, metricsClient decorator.MetricsClient) in.RunFollowUpRefreshesHandler {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if syncState == nil {
		panic("nil syncStateRepository")
	}
	if refresher == nil {
		panic("nil refresher")
	}
	return decorator.ApplyCommandDecorators[in.RunFollowUpRefreshes](
		runFollowUpRefreshesHandler{items: items, syncState: syncState, refresher: refresher}, logger, metricsClient)
}

// Handle takes every due follow-up, then refreshes each provider once
// unless a complete listing started after the latest due follow-up.
func (h runFollowUpRefreshesHandler) Handle(ctx context.Context, _ in.RunFollowUpRefreshes) error {
	items, err := h.items.RemoteItemsInStates(ctx, remoteitem.StateSubmitting, remoteitem.StateDeleting)
	if err != nil {
		return err
	}
	now := h.refresher.clock.Now()
	latestDue := map[remoteitem.ProviderID]time.Time{}
	var errs []error
	for _, candidate := range items {
		submission, ok := candidate.Submission()
		if !ok {
			continue
		}
		hasDue := false
		for _, due := range submission.FollowUpsDue {
			if due.After(now) {
				continue
			}
			hasDue = true
			if due.After(latestDue[candidate.ProviderID()]) {
				latestDue[candidate.ProviderID()] = due
			}
		}
		if !hasDue {
			continue
		}
		errs = append(errs, h.items.UpdateRemoteItem(ctx, candidate.ID(),
			func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				item.TakeDueFollowUps(now)
				return item, nil
			}))
	}
	for provider, due := range latestDue {
		lastSync, err := h.syncState.LastAuthoritativeSync(ctx, provider)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if lastSync.After(due) {
			continue
		}
		errs = append(errs, h.refresher.Refresh(ctx, provider, in.RefreshCauseFollowUp, ""))
	}
	return errors.Join(errs...)
}
