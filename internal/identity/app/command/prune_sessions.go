package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type pruneSessionsHandler struct {
	sessions out.SessionRepository
	clock    clock.Clock
}

func NewPruneSessionsHandler(sessions out.SessionRepository, clock clock.Clock, logger *slog.Logger, metricsClient decorator.MetricsClient) in.PruneSessionsHandler {
	if sessions == nil {
		panic("nil sessions")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.PruneSessions](pruneSessionsHandler{sessions: sessions, clock: clock}, logger, metricsClient)
}

func (h pruneSessionsHandler) Handle(ctx context.Context, _ in.PruneSessions) error {
	_, err := h.sessions.PruneSessions(ctx, h.clock.Now())
	return err
}
