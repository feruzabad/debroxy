package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// LinkOIDCIdentity links a user to an external identity (admin only,
// DEC-04). Only linked users can log in with OIDC.
type LinkOIDCIdentity struct {
	Actor   Actor
	UserID  string
	Issuer  string
	Subject string
}

type LinkOIDCIdentityHandler decorator.CommandHandler[LinkOIDCIdentity]
