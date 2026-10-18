package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

type refreshProviderStateHandler struct {
	refresher *Refresher
}

func NewRefreshProviderStateHandler(refresher *Refresher, logger *slog.Logger, metricsClient decorator.MetricsClient) in.RefreshProviderStateHandler {
	if refresher == nil {
		panic("nil refresher")
	}
	return decorator.ApplyCommandDecorators[in.RefreshProviderState](refreshProviderStateHandler{refresher: refresher}, logger, metricsClient)
}

func (h refreshProviderStateHandler) Handle(ctx context.Context, cmd in.RefreshProviderState) error {
	provider, err := remoteitem.NewProviderID(cmd.ProviderID)
	if err != nil {
		return remoteitem.ProviderNotFoundError{Provider: cmd.ProviderID}
	}
	userID := ""
	switch cmd.Cause {
	case in.RefreshCauseRead, in.RefreshCauseForced:
		actor, err := toDomainActor(cmd.Actor)
		if err != nil {
			return err
		}
		userID = actor.UserID
	case in.RefreshCauseWebhook, in.RefreshCauseSafety, in.RefreshCauseFollowUp, in.RefreshCauseWatch:
	default:
		return fmt.Errorf("unknown refresh cause %q", cmd.Cause)
	}
	return h.refresher.Refresh(ctx, provider, cmd.Cause, userID)
}
