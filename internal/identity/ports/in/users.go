package in

import (
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// Users lists all users (admin only).
type Users struct {
	Actor Actor
}

// UserSummary is the representation of a user for clients.
type UserSummary struct {
	ID            string
	Username      string
	Role          string
	Status        string
	PasswordLogin bool
	OIDCLinked    bool
	// OIDCIdentity is set only when the user has a linked OIDC identity (C14-05).
	OIDCIdentity *OIDCIdentity
	CreatedAt    time.Time
}

// OIDCIdentity is the linked external identity of a user (C14-05).
type OIDCIdentity struct {
	Issuer  string
	Subject string
}

type UsersHandler decorator.QueryHandler[Users, []UserSummary]
