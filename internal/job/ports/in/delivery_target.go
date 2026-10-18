package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// DeliveryTarget resolves what delivery streams for a job (spec §5.9). Set
// FileID for a file, or Bundle for the provider-built archive. The actor
// must be the job's owner.
type DeliveryTarget struct {
	Actor  Actor
	JobID  string
	FileID string
	Bundle bool
}

type DeliveryTargetHandler decorator.QueryHandler[DeliveryTarget, DeliveryTargetResult]
