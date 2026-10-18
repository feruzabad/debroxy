// Package scheduler is the driving adapter that runs job's periodic
// commands (spec §5.6 "Periodic"): follow-up and safety refreshes,
// ExpireSubmissions, ResumeDeferredSubmissions, DetectStalls, and the
// hourly PruneExpiredRecords.
package scheduler

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// Scheduler ticks the periodic commands. The decorators log failures, so a
// failed command never stops the loop.
type Scheduler struct {
	commands in.Commands
	interval time.Duration
	// pruneEvery runs PruneExpiredRecords on every n-th tick, starting
	// with the first.
	pruneEvery int
}

// pruneInterval is how often expired records are pruned.
const pruneInterval = time.Hour

func NewScheduler(commands in.Commands, interval time.Duration) Scheduler {
	if commands.ExpireSubmissions == nil {
		panic("nil commands")
	}
	if interval <= 0 {
		panic("non-positive interval")
	}
	return Scheduler{commands: commands, interval: interval, pruneEvery: max(int(pruneInterval/interval), 1)}
}

// Run ticks at once (resuming work left by a restart) and then every
// interval, until ctx ends.
func (s Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for tick := 0; ; tick++ {
		s.Tick(ctx)
		if tick%s.pruneEvery == 0 {
			_ = s.commands.PruneExpiredRecords.Handle(ctx, in.PruneExpiredRecords{}) // logged by the decorator
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick runs every periodic command once, in order.
func (s Scheduler) Tick(ctx context.Context) {
	steps := []func() error{
		func() error { return s.commands.RunFollowUpRefreshes.Handle(ctx, in.RunFollowUpRefreshes{}) },
		func() error { return s.commands.RunSafetyRefreshes.Handle(ctx, in.RunSafetyRefreshes{}) },
		func() error { return s.commands.ExpireSubmissions.Handle(ctx, in.ExpireSubmissions{}) },
		func() error { return s.commands.ResumeDeferredSubmissions.Handle(ctx, in.ResumeDeferredSubmissions{}) },
		func() error { return s.commands.DetectStalls.Handle(ctx, in.DetectStalls{}) },
	}
	for _, step := range steps {
		if ctx.Err() != nil {
			return
		}
		_ = step() // logged by the decorators
	}
}
