package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type logInWithOIDCHandler struct {
	users    out.UserRepository
	sessions out.SessionRepository
	factory  session.Factory
	clock    clock.Clock
}

func NewLogInWithOIDCHandler(
	users out.UserRepository, sessions out.SessionRepository, factory session.Factory, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.LogInWithOIDCHandler {
	switch {
	case users == nil:
		panic("nil users")
	case sessions == nil:
		panic("nil sessions")
	case factory.IsZero():
		panic("zero session factory")
	case clock == nil:
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.LogInWithOIDC](logInWithOIDCHandler{
		users: users, sessions: sessions, factory: factory, clock: clock,
	}, logger, metricsClient)
}

// Handle logs in only an active user linked beforehand (DEC-04); every
// other case is the uniform invalid-credentials.
func (h logInWithOIDCHandler) Handle(ctx context.Context, cmd in.LogInWithOIDC) error {
	sessionID, err := session.NewID(cmd.SessionID)
	if err != nil {
		return err
	}
	identity, err := user.NewOIDCIdentity(cmd.Issuer, cmd.Subject)
	if err != nil {
		return user.ErrInvalidCredentials
	}
	account, err := h.users.UserByOIDCIdentity(ctx, identity)
	if user.IsNotFound(err) {
		return user.ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	if !account.CanLogInWithOIDC() {
		return user.ErrInvalidCredentials
	}
	started, err := h.factory.NewSession(sessionID, account.ID().String(), cmd.SessionToken, account.SessionGeneration(), h.clock.Now())
	if err != nil {
		return err
	}
	return h.sessions.AddSession(ctx, started)
}
