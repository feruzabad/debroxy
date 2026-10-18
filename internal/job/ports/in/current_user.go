package in

import (
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// CurrentUser returns the actor's own user record, read from identity
// through UserDirectory (C9-02). GET /api/v1/me shows it with QuotaUsage.
type CurrentUser struct {
	Actor Actor
}

// UserView is a user as identity reports it.
type UserView struct {
	ID            string
	Username      string
	Role          string
	Status        string
	PasswordLogin bool
	OIDCLinked    bool
	CreatedAt     time.Time
}

type CurrentUserHandler decorator.QueryHandler[CurrentUser, UserView]
