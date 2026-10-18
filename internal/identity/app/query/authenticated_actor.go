package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type authenticatedActorHandler struct {
	readModel   out.AuthenticatedActorReadModel
	clock       clock.Clock
	idleTimeout time.Duration
}

func NewAuthenticatedActorHandler(
	readModel out.AuthenticatedActorReadModel, idleTimeout time.Duration, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.AuthenticatedActorHandler {
	if readModel == nil {
		panic("nil readModel")
	}
	if idleTimeout <= 0 {
		panic("non-positive idle timeout")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyQueryDecorators[in.AuthenticatedActor, in.AuthenticatedActorResult](
		authenticatedActorHandler{readModel: readModel, clock: clock, idleTimeout: idleTimeout}, logger, metricsClient)
}

// Handle accepts a session only if it is active, its user is active, and it
// was started under the user's current session generation (revocation).
func (h authenticatedActorHandler) Handle(ctx context.Context, query in.AuthenticatedActor) (in.AuthenticatedActorResult, error) {
	found, err := h.readModel.SessionForAuthentication(ctx, session.HashToken(query.SessionToken).String())
	if err != nil {
		return in.AuthenticatedActorResult{}, err
	}
	now := h.clock.Now()
	valid := session.IsActive(found.RevokedAt, found.AbsoluteExpiresAt, found.IdleExpiresAt, now) &&
		found.UserStatus == user.StatusActive.String() &&
		found.SessionGeneration == found.UserSessionGeneration
	if !valid {
		return in.AuthenticatedActorResult{}, session.ErrSessionExpired
	}
	idleExpiresAt := found.IdleExpiresAt
	if session.IsTouchDue(found.LastSeenAt, now) {
		// The touch the caller will send moves the idle expiry to now +
		// idleTimeout, never past the absolute expiry (Session.Touch).
		idleExpiresAt = session.ExpiresAt(found.AbsoluteExpiresAt, now.Add(h.idleTimeout))
	}
	return in.AuthenticatedActorResult{
		UserID:                   found.UserID,
		Role:                     found.Role,
		SessionID:                found.SessionID,
		SessionExpiresAt:         session.ExpiresAt(found.AbsoluteExpiresAt, found.IdleExpiresAt),
		SessionIdleExpiresAt:     idleExpiresAt,
		SessionAbsoluteExpiresAt: found.AbsoluteExpiresAt,
		TouchDue:                 session.IsTouchDue(found.LastSeenAt, now),
	}, nil
}
