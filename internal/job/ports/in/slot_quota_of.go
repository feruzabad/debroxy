package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// SlotQuotaOf reports a user's slot quota to an admin (spec §10.5, C14-01).
// job does not own users, so an unknown user ID answers the instance default
// with SlotsUsed 0.
type SlotQuotaOf struct {
	Actor  Actor
	UserID string
}

// SlotQuotaResult is the effective slot quota of a user.
type SlotQuotaResult struct {
	// MaxActiveJobs is the effective limit: the stored SlotQuota row, or the
	// instance default when IsDefault is true (ADR 0006 rule 11).
	MaxActiveJobs int
	// IsDefault is true when the user has no SlotQuota row and the instance
	// default applies.
	IsDefault bool
	// SlotsUsed is the ActiveChargedItemCount of the user (ADR 0006 rule 12).
	SlotsUsed int
}

type SlotQuotaOfHandler decorator.QueryHandler[SlotQuotaOf, SlotQuotaResult]
