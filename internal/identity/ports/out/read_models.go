package out

import (
	"context"

	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// AuthenticatedActorReadModel finds a session and its user by token hash.
// An unknown hash is reported as session.ErrSessionExpired.
type AuthenticatedActorReadModel interface {
	SessionForAuthentication(ctx context.Context, tokenHash string) (in.SessionForAuthentication, error)
}

type UsersReadModel interface {
	Users(ctx context.Context) ([]in.UserSummary, error)
}

// UserDetailsReadModel reports an unknown ID as user.NotFoundError.
type UserDetailsReadModel interface {
	UserDetails(ctx context.Context, userID string) (in.UserSummary, error)
}

type UserStatusReadModel interface {
	UserStatus(ctx context.Context, userID string) (in.UserStatusResult, error)
}
