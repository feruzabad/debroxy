package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// JobDetails returns one unreleased job of the actor (any job for an admin).
type JobDetails struct {
	Actor Actor
	JobID string
}

type JobDetailsHandler decorator.QueryHandler[JobDetails, JobView]
