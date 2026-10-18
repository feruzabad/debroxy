package command

import (
	"fmt"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// ErrJobIDConflict: the JobID is taken by another user's job or by a job
// for other content (DEC-38).
var ErrJobIDConflict = commonerrors.NewConflictError("this job ID is already used for other content", "job-id-conflict")

// SubmitRateExceededError: the user's submit rate for the class is used up (DEC-24).
type SubmitRateExceededError struct {
	Class              string
	PerHour            int
	RetryAfterDuration time.Duration
}

func (e SubmitRateExceededError) Error() string {
	return fmt.Sprintf("submit rate reached: at most %d %s submits per hour", e.PerHour, e.Class)
}
func (e SubmitRateExceededError) Slug() string { return "submit-rate-exceeded" }
func (e SubmitRateExceededError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeQuotaExceeded
}
func (e SubmitRateExceededError) RetryAfter() time.Duration { return e.RetryAfterDuration }

// RefreshRateExceededError: the user's forced refreshes per minute are used up (C6-05).
type RefreshRateExceededError struct {
	PerMinute          int
	RetryAfterDuration time.Duration
}

func (e RefreshRateExceededError) Error() string {
	return fmt.Sprintf("refresh rate reached: at most %d forced refreshes per minute", e.PerMinute)
}
func (e RefreshRateExceededError) Slug() string { return "refresh-rate-exceeded" }
func (e RefreshRateExceededError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeQuotaExceeded
}
func (e RefreshRateExceededError) RetryAfter() time.Duration { return e.RetryAfterDuration }
