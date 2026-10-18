package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ResetUserPassword sets a new password for a user and revokes their sessions (admin only).
type ResetUserPassword struct {
	Actor       Actor
	UserID      string
	NewPassword string
}

type ResetUserPasswordHandler decorator.CommandHandler[ResetUserPassword]
