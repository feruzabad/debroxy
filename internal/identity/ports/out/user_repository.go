// Package out holds the driven ports of the identity context: what it needs
// from storage and from the outside world.
package out

import (
	"context"

	"github.com/feruzabad/debroxy/internal/identity/domain/user"
)

// UserRepository stores User aggregates. Both write methods run their
// function inside one write transaction (ddd-rules R-2) and pass it facts
// about other users through UserPolicyReads (R-10).
type UserRepository interface {
	// CreateUser stores the user returned by createFn. A taken username is
	// reported as user.UsernameTakenError.
	CreateUser(ctx context.Context, createFn func(ctx context.Context, reads UserPolicyReads) (*user.User, error)) error
	// UpdateUser loads a user, applies updateFn and stores the result. An
	// unknown ID is reported as user.NotFoundError.
	UpdateUser(ctx context.Context, id user.ID, updateFn func(ctx context.Context, u *user.User, reads UserPolicyReads) (*user.User, error)) error
	UserByID(ctx context.Context, id user.ID) (*user.User, error)
	UserByUsername(ctx context.Context, username user.Username) (*user.User, error)
	// UserByOIDCIdentity reports an unlinked identity as user.NotFoundError.
	UserByOIDCIdentity(ctx context.Context, identity user.OIDCIdentity) (*user.User, error)
}

// UserPolicyReads answers questions about other users inside the write
// transaction of UserRepository. It has no write methods.
type UserPolicyReads interface {
	UserCount(ctx context.Context) (int, error)
	ActiveAdminCount(ctx context.Context) (int, error)
	// OIDCIdentityHolder returns the ID of the user linked to the identity;
	// found is false when nobody is.
	OIDCIdentityHolder(ctx context.Context, identity user.OIDCIdentity) (userID string, found bool, err error)
}
