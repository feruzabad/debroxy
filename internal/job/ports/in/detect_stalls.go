package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// DetectStalls flags items without progress beyond the stall threshold (spec §5.3).
type DetectStalls struct{}

type DetectStallsHandler decorator.CommandHandler[DetectStalls]
