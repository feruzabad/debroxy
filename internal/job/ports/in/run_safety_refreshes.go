package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// RunSafetyRefreshes refreshes each provider whose last refresh is older
// than the safety interval: short while any of its items is active, long
// otherwise (spec §5.6).
type RunSafetyRefreshes struct{}

type RunSafetyRefreshesHandler decorator.CommandHandler[RunSafetyRefreshes]
