package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// UnlinkOIDCIdentity removes a user's external identity (admin only). A
// user without a password keeps it (no-login-method).
type UnlinkOIDCIdentity struct {
	Actor  Actor
	UserID string
}

type UnlinkOIDCIdentityHandler decorator.CommandHandler[UnlinkOIDCIdentity]
