package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ReleaseJob removes the actor's claim (owner, or admin for any job).
type ReleaseJob struct {
	Actor Actor
	JobID string
}

type ReleaseJobHandler decorator.CommandHandler[ReleaseJob]
