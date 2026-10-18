package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/job/adapters/in/scheduler"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// recorder records which periodic command ran.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type command[C any] struct {
	name string
	rec  *recorder
	err  error
}

func (c command[C]) Handle(context.Context, C) error {
	c.rec.add(c.name)
	return c.err
}

func commands(rec *recorder) in.Commands {
	return in.Commands{
		RunFollowUpRefreshes:      command[in.RunFollowUpRefreshes]{"follow-ups", rec, errors.New("provider down")},
		RunSafetyRefreshes:        command[in.RunSafetyRefreshes]{"safety", rec, nil},
		ExpireSubmissions:         command[in.ExpireSubmissions]{"expire", rec, nil},
		ResumeDeferredSubmissions: command[in.ResumeDeferredSubmissions]{"resume", rec, nil},
		DetectStalls:              command[in.DetectStalls]{"stalls", rec, nil},
		PruneExpiredRecords:       command[in.PruneExpiredRecords]{"prune", rec, nil},
	}
}

func TestTick_runs_every_periodic_command_even_after_a_failure(t *testing.T) {
	t.Parallel()
	rec := &recorder{}

	scheduler.NewScheduler(commands(rec), time.Minute).Tick(context.Background())

	assert.Equal(t, []string{"follow-ups", "safety", "expire", "resume", "stalls"}, rec.names())
}

func TestRun_ticks_at_once_and_stops_with_its_context(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		scheduler.NewScheduler(commands(rec), 10*time.Millisecond).Run(ctx)
		close(done)
	}()
	assert.Eventually(t, func() bool { return len(rec.names()) >= 11 }, time.Second, time.Millisecond, "two ticks")
	cancel()
	assert.Equal(t, "prune", rec.names()[5], "pruning runs on the first tick")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
