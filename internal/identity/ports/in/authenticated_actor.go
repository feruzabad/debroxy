package in

import (
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// AuthenticatedActor resolves a session token to the user behind it.
type AuthenticatedActor struct {
	SessionToken string
}

type AuthenticatedActorResult struct {
	UserID    string
	Role      string
	SessionID string
	// SessionExpiresAt is when the session ends unless it is used again.
	SessionExpiresAt time.Time
	// SessionIdleExpiresAt is the idle expiry after this request's touch:
	// the effective expiry of spec §10.4.
	SessionIdleExpiresAt time.Time
	// SessionAbsoluteExpiresAt is when the session ends at the latest.
	SessionAbsoluteExpiresAt time.Time
	// TouchDue tells the caller to send TouchSession.
	TouchDue bool
}

type AuthenticatedActorHandler decorator.QueryHandler[AuthenticatedActor, AuthenticatedActorResult]

// SessionForAuthentication is what the read model knows about a session
// and its user; the query handler decides validity from it.
type SessionForAuthentication struct {
	SessionID             string
	UserID                string
	Role                  string
	UserStatus            string
	UserSessionGeneration int
	// SessionGeneration is the user's session generation when the session started.
	SessionGeneration int
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
	IdleExpiresAt     time.Time
	// RevokedAt is zero while the session is not revoked.
	RevokedAt time.Time
}
