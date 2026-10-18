package httpclient

import (
	"errors"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

// ErrCircuitOpen: recent calls failed repeatedly, so calls fail fast until
// the open period ends (NFR-09).
var ErrCircuitOpen = errors.New("circuit breaker open after repeated failures")

// CircuitBreakerConfig: FailureThreshold consecutive failures open the
// circuit for OpenDuration; then one trial call decides.
type CircuitBreakerConfig struct {
	FailureThreshold int
	OpenDuration     time.Duration
}

type circuitBreaker struct {
	mu        sync.Mutex
	cfg       CircuitBreakerConfig
	clock     clock.Clock
	failures  int
	openUntil time.Time
	trialing  bool
}

func (b *circuitBreaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < b.cfg.FailureThreshold {
		return nil
	}
	if b.clock.Now().Before(b.openUntil) || b.trialing {
		return ErrCircuitOpen
	}
	b.trialing = true
	return nil
}

// record counts a finished call. Network errors and 5xx answers are
// failures; anything else the server answered counts as success.
func (b *circuitBreaker) record(failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trialing = false
	if !failed {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures >= b.cfg.FailureThreshold {
		b.openUntil = b.clock.Now().Add(b.cfg.OpenDuration)
	}
}

// cancel ends a trial without a result, for calls that were never sent.
func (b *circuitBreaker) cancel() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trialing = false
}
