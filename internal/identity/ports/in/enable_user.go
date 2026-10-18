package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// EnableUser lets a disabled user log in again (admin only).
type EnableUser struct {
	Actor  Actor
	UserID string
}

type EnableUserHandler decorator.CommandHandler[EnableUser]
