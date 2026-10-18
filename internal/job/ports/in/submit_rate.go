package in

import (
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// SubmitRate reports the caller's remaining submit allowance per class
// within the rolling hour (spec §10.4, ADR 0013 rule 13).
type SubmitRate struct {
	Actor Actor
}

// SubmitRateClassResult is one rate class: uncached or cached.
type SubmitRateClassResult struct {
	Used int
	// PerHour is the class's limit from configuration.
	PerHour int
	// ResetsAt is when the oldest counted submit leaves the window; nil when Used is 0.
	ResetsAt *time.Time
}

type SubmitRateResult struct {
	Uncached SubmitRateClassResult
	Cached   SubmitRateClassResult
}

type SubmitRateHandler decorator.QueryHandler[SubmitRate, SubmitRateResult]

// SubmitRateClassUsage is what storage knows about one class in the rolling
// hour: how many accepted submits there are and the earliest accepted_at
// (zero when there are none). The query handler turns Oldest into ResetsAt.
type SubmitRateClassUsage struct {
	Used   int
	Oldest time.Time
}

type SubmitRateUsageResult struct {
	Uncached SubmitRateClassUsage
	Cached   SubmitRateClassUsage
}
