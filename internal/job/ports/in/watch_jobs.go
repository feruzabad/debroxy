package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// Event Stream scopes (spec §5.11).
const (
	WatchScopeOwn = "own"
	WatchScopeAll = "all"
)

// WatchJobs streams snapshots of the jobs the actor may see (spec §5.11,
// ADR 0021 rule 6). Scope "all" is for admins only (forbidden otherwise).
// Over a stream limit it answers too-many-streams with a retry hint.
type WatchJobs struct {
	Actor Actor
	Scope string
}

// JobWatch is a running WatchJobs. Events is closed when the watch ends:
// its context ended, the service shut down, a read failed, or the change
// feed overran it; the client then reconnects and refetches.
type JobWatch struct {
	Events <-chan JobEvent
}

// JobEvent is one Event Stream event; exactly one field is set.
type JobEvent struct {
	Upserted      *JobView
	Released      *JobReleasedEvent
	Quota         *QuotaUsageResult
	ProviderStale *ProviderStaleEvent
}

type JobReleasedEvent struct {
	JobID        string
	HistoryEntry HistoryEntry
}

type ProviderStaleEvent struct {
	ProviderID string
	Stale      bool
}

type WatchJobsHandler decorator.QueryHandler[WatchJobs, JobWatch]
