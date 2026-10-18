package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ChangeUserRole sets a user's role (admin only; last-admin rule).
type ChangeUserRole struct {
	Actor  Actor
	UserID string
	Role   string
}

type ChangeUserRoleHandler decorator.CommandHandler[ChangeUserRole]
