package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// AllJobs lists every unreleased job (admin only).
type AllJobs struct {
	Actor Actor
}

type AllJobsHandler decorator.QueryHandler[AllJobs, []JobView]
