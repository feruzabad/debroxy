package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// SetSlotQuota sets a user's maximum of charged active jobs (admin only).
type SetSlotQuota struct {
	Actor         Actor
	UserID        string
	MaxActiveJobs int
}

type SetSlotQuotaHandler decorator.CommandHandler[SetSlotQuota]
