package memory_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/identity/adapters/out/memory"
)

var start = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func TestMemoryLoginAttemptLimiter_backs_off_after_five_failures(t *testing.T) {
	t.Parallel()
	limiter := memory.NewMemoryLoginAttemptLimiter()

	for range 4 {
		limiter.RecordFailure("alice", "203.0.113.7", start)
	}
	assert.Zero(t, limiter.RetryAfter("alice", "203.0.113.7", start), "four failures are free")

	limiter.RecordFailure("alice", "203.0.113.7", start)
	assert.Equal(t, time.Second, limiter.RetryAfter("alice", "198.51.100.1", start), "the username is throttled from any IP")
	assert.Equal(t, time.Second, limiter.RetryAfter("bob", "203.0.113.7", start), "the IP is throttled for any username")

	limiter.RecordFailure("alice", "203.0.113.7", start)
	assert.Equal(t, 2*time.Second, limiter.RetryAfter("alice", "203.0.113.7", start), "each failure doubles the wait")
	assert.Equal(t, time.Second, limiter.RetryAfter("alice", "203.0.113.7", start.Add(time.Second)))
}

func TestMemoryLoginAttemptLimiter_caps_the_wait(t *testing.T) {
	t.Parallel()
	limiter := memory.NewMemoryLoginAttemptLimiter()

	for range 40 {
		limiter.RecordFailure("alice", "", start)
	}

	assert.Equal(t, 15*time.Minute, limiter.RetryAfter("alice", "", start))
}

func TestMemoryLoginAttemptLimiter_success_resets_and_usernames_ignore_case(t *testing.T) {
	t.Parallel()
	limiter := memory.NewMemoryLoginAttemptLimiter()
	for range 6 {
		limiter.RecordFailure("Alice", "203.0.113.7", start)
	}

	limiter.RecordSuccess("alice", "203.0.113.7")

	assert.Zero(t, limiter.RetryAfter("ALICE", "203.0.113.7", start))
}
