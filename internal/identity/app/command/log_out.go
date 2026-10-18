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

type logOutHandler struct {
	sessions out.SessionRepository
	clock    clock.Clock
}

func NewLogOutHandler(sessions out.SessionRepository, clock clock.Clock, logger *slog.Logger, metricsClient decorator.MetricsClient) in.LogOutHandler {
	if sessions == nil {
		panic("nil sessions")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.LogOut](logOutHandler{sessions: sessions, clock: clock}, logger, metricsClient)
}

func (h logOutHandler) Handle(ctx context.Context, cmd in.LogOut) error {
	sessionID, err := session.NewID(cmd.Actor.SessionID)
	if err != nil {
		return err
	}
	return h.sessions.UpdateSession(ctx, sessionID, func(_ context.Context, s *session.Session) (*session.Session, error) {
		if !s.BelongsTo(cmd.Actor.UserID) {
			return nil, session.NotFoundError{ID: sessionID.String()}
		}
		s.Revoke(h.clock.Now())
		return s, nil
	})
}
