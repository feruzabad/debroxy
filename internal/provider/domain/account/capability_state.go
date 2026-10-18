package account

import (
	"errors"
	"time"
)

// ReasonPlan: the account's plan does not allow the capability
// (TorBox PLAN_RESTRICTED_FEATURE).
const ReasonPlan = "plan"

// UnavailableRetryAfter: an unavailable capability is tried again at most
// once per this period, never per user request (ADR 0003 rule 8).
const UnavailableRetryAfter = 24 * time.Hour

// CapabilityState is what debroxy has learned about one capability of one
// account: available{since}, unavailable{reason, since} or unverified.
type CapabilityState struct {
	status Status
	reason string
	since  time.Time
}

func Available(since time.Time) CapabilityState {
	return CapabilityState{status: StatusAvailable, since: since}
}

func Unavailable(reason string, since time.Time) CapabilityState {
	return CapabilityState{status: StatusUnavailable, reason: reason, since: since}
}

func Unverified() CapabilityState {
	return CapabilityState{status: StatusUnverified}
}

// UnmarshalCapabilityStateFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalCapabilityStateFromDatabase(status Status, reason string, since time.Time) (CapabilityState, error) {
	switch status {
	case StatusAvailable:
		return Available(since), nil
	case StatusUnavailable:
		if reason == "" {
			return CapabilityState{}, errors.New("unavailable capability without a reason")
		}
		return Unavailable(reason, since), nil
	case StatusUnverified:
		return Unverified(), nil
	}
	return CapabilityState{}, errors.New("empty capability status")
}

func (s CapabilityState) Status() Status   { return s.status }
func (s CapabilityState) Reason() string   { return s.reason }
func (s CapabilityState) Since() time.Time { return s.since }

// blocksAt: unavailable and younger than UnavailableRetryAfter.
func (s CapabilityState) blocksAt(now time.Time) bool {
	return s.status == StatusUnavailable && now.Sub(s.since) < UnavailableRetryAfter
}
