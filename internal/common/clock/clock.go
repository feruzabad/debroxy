// Package clock is the time source of the application layer (ddd-rules D-10).
// Domain code never reads the clock; it receives `now` as a parameter.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// System reads the wall clock, in UTC.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a clock for tests that moves only when told to. It is safe for
// concurrent use.
type Fixed struct {
	mu  sync.Mutex
	now time.Time
}

func NewFixed(now time.Time) *Fixed {
	return &Fixed{now: now}
}

func (c *Fixed) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to now.
func (c *Fixed) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// Advance moves the clock forward by d.
func (c *Fixed) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
