// Package out holds the driven ports of the delivery context.
package out

import "context"

// JobTargets is what delivery needs from job (spec §3.2, DEP-4). A job
// that is released, not the user's own, not ready, or without the target
// is an error with job's slug (job-not-found, target-not-available).
type JobTargets interface {
	DeliveryTarget(ctx context.Context, userID, jobID string, target TargetRequest) (Target, error)
	// AllTargets lists every file target in stored order, or the bundle
	// target when the item has no per-file delivery.
	AllTargets(ctx context.Context, userID, jobID string) ([]Target, error)
}

// TargetRequest names a file or the bundle.
type TargetRequest struct {
	FileID string
	Bundle bool
}

// Target is a resolved delivery target. ProviderItemID stays inside
// debroxy: it only travels to provider.
type Target struct {
	ProviderID     string
	Kind           string
	ProviderItemID string
	FileID         string
	Bundle         bool
	// Name is the file's path or, for a bundle, the item's name.
	Name           string
	RangeSupported bool
}
