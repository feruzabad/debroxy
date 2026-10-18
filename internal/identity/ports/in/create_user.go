package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// CreateUser adds a user with a password, an OIDC identity, or both
// (admin only, spec §4.2). Empty fields mean "none".
type CreateUser struct {
	Actor       Actor
	UserID      string
	Username    string
	Role        string
	Password    string
	OIDCIssuer  string
	OIDCSubject string
}

type CreateUserHandler decorator.CommandHandler[CreateUser]
