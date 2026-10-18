package out

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/identity/domain/session"
)

// SessionRepository stores Session aggregates.
type SessionRepository interface {
	AddSession(ctx context.Context, s *session.Session) error
	// UpdateSession loads a session, applies updateFn and stores the result
	// in one write transaction. An unknown ID is reported as
	// session.NotFoundError.
	UpdateSession(ctx context.Context, id session.ID, updateFn func(ctx context.Context, s *session.Session) (*session.Session, error)) error
	// PruneSessions deletes every session that is no longer active at now
	// (revoked, or past its absolute or idle expiry) and reports how many.
	PruneSessions(ctx context.Context, now time.Time) (int, error)
}
