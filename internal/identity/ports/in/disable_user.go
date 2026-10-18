package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// DisableUser blocks a user and revokes their sessions (admin only; last-admin rule).
type DisableUser struct {
	Actor  Actor
	UserID string
}

type DisableUserHandler decorator.CommandHandler[DisableUser]
