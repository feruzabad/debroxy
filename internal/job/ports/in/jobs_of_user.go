package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// JobsOfUser lists the actor's unreleased jobs, newest first.
type JobsOfUser struct {
	Actor Actor
}

type JobsOfUserHandler decorator.QueryHandler[JobsOfUser, []JobView]
