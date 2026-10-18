// Package scheduler is the driving adapter that runs identity's periodic
// commands: pruning dead sessions.
package scheduler

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// Scheduler ticks PruneSessions; the decorator logs failures.
type Scheduler struct {
	commands in.Commands
	interval time.Duration
}

func NewScheduler(commands in.Commands, interval time.Duration) Scheduler {
	if commands.PruneSessions == nil {
		panic("nil commands")
	}
	if interval <= 0 {
		panic("non-positive interval")
	}
	return Scheduler{commands: commands, interval: interval}
}

// Run ticks at once and then every interval, until ctx ends.
func (s Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		_ = s.commands.PruneSessions.Handle(ctx, in.PruneSessions{}) // logged by the decorator
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
