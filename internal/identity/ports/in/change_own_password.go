package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ChangeOwnPassword needs the current password and revokes the actor's
// other sessions; the session making the change stays valid.
type ChangeOwnPassword struct {
	Actor           Actor
	CurrentPassword string
	NewPassword     string
}

type ChangeOwnPasswordHandler decorator.CommandHandler[ChangeOwnPassword]
