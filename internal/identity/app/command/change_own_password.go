package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type changeOwnPasswordHandler struct {
	users    out.UserRepository
	sessions out.SessionRepository
	hasher   out.PasswordHasher
}

func NewChangeOwnPasswordHandler(
	users out.UserRepository, sessions out.SessionRepository, hasher out.PasswordHasher,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.ChangeOwnPasswordHandler {
	if users == nil {
		panic("nil users")
	}
	if sessions == nil {
		panic("nil sessions")
	}
	if hasher == nil {
		panic("nil hasher")
	}
	return decorator.ApplyCommandDecorators[in.ChangeOwnPassword](
		changeOwnPasswordHandler{users: users, sessions: sessions, hasher: hasher}, logger, metricsClient)
}

// Handle changes the password in one transaction on the User (which revokes
// every session), then keeps the current session in a second transaction
// on that Session. If the second step fails, the actor is logged out too,
// which is the safe direction.
func (h changeOwnPasswordHandler) Handle(ctx context.Context, cmd in.ChangeOwnPassword) error {
	actor, err := toDomainActor(cmd.Actor)
	if err != nil {
		return err
	}
	sessionID, err := session.NewID(cmd.Actor.SessionID)
	if err != nil {
		return err
	}
	newPassword, err := user.NewPassword(cmd.NewPassword)
	if err != nil {
		return err
	}
	current, err := h.users.UserByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if current.PasswordHash().IsZero() {
		return errWrongCurrentPassword
	}
	matches, err := h.hasher.Verify(current.PasswordHash(), cmd.CurrentPassword)
	if err != nil {
		return err
	}
	if !matches {
		return errWrongCurrentPassword
	}
	newHash, err := h.hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	var generation int
	err = h.users.UpdateUser(ctx, actor.UserID, func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
		if err := u.ChangePassword(newHash); err != nil {
			return nil, err
		}
		generation = u.SessionGeneration()
		return u, nil
	})
	if err != nil {
		return err
	}
	return h.sessions.UpdateSession(ctx, sessionID, func(_ context.Context, s *session.Session) (*session.Session, error) {
		if !s.BelongsTo(actor.UserID.String()) {
			return nil, session.NotFoundError{ID: sessionID.String()}
		}
		if err := s.FollowUserSessionGeneration(generation); err != nil {
			return nil, err
		}
		return s, nil
	})
}
