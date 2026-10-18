package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ExpireSubmissions fails submissions that can no longer succeed (spec §5.4,
// ADR 0005 rule 14). Run every minute by the scheduler.
type ExpireSubmissions struct{}

type ExpireSubmissionsHandler decorator.CommandHandler[ExpireSubmissions]
