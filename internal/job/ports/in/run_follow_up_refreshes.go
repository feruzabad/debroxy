package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// RunFollowUpRefreshes takes the due follow-ups of ambiguous creates and
// refreshes their providers, unless a complete listing ran since they were
// due (spec §5.6).
type RunFollowUpRefreshes struct{}

type RunFollowUpRefreshesHandler decorator.CommandHandler[RunFollowUpRefreshes]
