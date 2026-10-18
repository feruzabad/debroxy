package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// LogOut revokes the actor's current session.
type LogOut struct {
	Actor Actor
}

type LogOutHandler decorator.CommandHandler[LogOut]
