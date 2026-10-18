package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// JobHistory lists the actor's released jobs.
type JobHistory struct {
	Actor Actor
}

type JobHistoryHandler decorator.QueryHandler[JobHistory, []HistoryEntry]
