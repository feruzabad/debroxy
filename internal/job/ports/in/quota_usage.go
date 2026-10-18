package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// QuotaUsage reports the actor's charged slots and limit (spec §5.4).
type QuotaUsage struct {
	Actor Actor
}

type QuotaUsageHandler decorator.QueryHandler[QuotaUsage, QuotaUsageResult]
