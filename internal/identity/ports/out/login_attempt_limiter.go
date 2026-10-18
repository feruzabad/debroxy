package out

import "time"

// LoginAttemptLimiter throttles password logins per username and per
// client IP (DEC-05): exponential back-off after repeated failures.
type LoginAttemptLimiter interface {
	// RetryAfter is how long the next attempt must wait; zero means allowed.
	RetryAfter(username, clientIP string, now time.Time) time.Duration
	RecordFailure(username, clientIP string, now time.Time)
	RecordSuccess(username, clientIP string)
}
