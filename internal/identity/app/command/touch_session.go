package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type touchSessionHandler struct {
	sessions out.SessionRepository
	factory  session.Factory
	clock    clock.Clock
}

func NewTouchSessionHandler(
	sessions out.SessionRepository, factory session.Factory, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.TouchSessionHandler {
	if sessions == nil {
		panic("nil sessions")
	}
	if factory.IsZero() {
		panic("zero session factory")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.TouchSession](
		touchSessionHandler{sessions: sessions, factory: factory, clock: clock}, logger, metricsClient)
}

func (h touchSessionHandler) Handle(ctx context.Context, cmd in.TouchSession) error {
	sessionID, err := session.NewID(cmd.SessionID)
	if err != nil {
		return err
	}
	return h.sessions.UpdateSession(ctx, sessionID, func(_ context.Context, s *session.Session) (*session.Session, error) {
		s.Touch(h.clock.Now(), h.factory.IdleTimeout())
		return s, nil
	})
}
