// Package slotquota is the SlotQuota aggregate (spec §5.1, ADR 0006 rule 11):
// the maximum number of active items a user may hold a slot charge on.
package slotquota

import (
	"errors"
	"fmt"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// SlotQuota is one user's limit. A user without a stored quota uses the
// configured default.
type SlotQuota struct {
	userID        string
	maxActiveJobs int
}

func NewSlotQuota(userID string, maxActiveJobs int) (SlotQuota, error) {
	if userID == "" {
		return SlotQuota{}, errors.New("empty user ID")
	}
	if maxActiveJobs < 0 {
		return SlotQuota{}, InvalidSlotQuotaError{Given: maxActiveJobs}
	}
	return SlotQuota{userID: userID, maxActiveJobs: maxActiveJobs}, nil
}

func (q SlotQuota) UserID() string     { return q.userID }
func (q SlotQuota) MaxActiveJobs() int { return q.maxActiveJobs }

// AllowsNewCharge: a new charged claim is allowed only while slotsUsed is
// below the limit; at equality it is refused (ADR 0006 rule 10).
func (q SlotQuota) AllowsNewCharge(slotsUsed int) error {
	if slotsUsed < q.maxActiveJobs {
		return nil
	}
	return QuotaExceededError{SlotsUsed: slotsUsed, MaxActiveJobs: q.maxActiveJobs}
}

type InvalidSlotQuotaError struct{ Given int }

func (e InvalidSlotQuotaError) Error() string {
	return fmt.Sprintf("slot quota must not be negative, got %d", e.Given)
}
func (e InvalidSlotQuotaError) Slug() string { return "invalid-slot-quota" }
func (e InvalidSlotQuotaError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

// QuotaExceededError: the user already uses all slots.
type QuotaExceededError struct {
	SlotsUsed     int
	MaxActiveJobs int
}

func (e QuotaExceededError) Error() string {
	return fmt.Sprintf("slot quota reached: %d of %d active jobs charged to you", e.SlotsUsed, e.MaxActiveJobs)
}
func (e QuotaExceededError) Slug() string { return "slot-quota-exceeded" }
func (e QuotaExceededError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeQuotaExceeded
}
