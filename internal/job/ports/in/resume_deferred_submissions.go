package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ResumeDeferredSubmissions re-runs creates whose blocker is deleted or
// whose NotBefore has passed (spec §5.4, C6-02). Run every minute.
type ResumeDeferredSubmissions struct{}

type ResumeDeferredSubmissionsHandler decorator.CommandHandler[ResumeDeferredSubmissions]
