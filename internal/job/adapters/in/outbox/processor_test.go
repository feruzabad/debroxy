package outbox_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/job/adapters/in/outbox"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

type countingProcess struct{ runs atomic.Int32 }

func (c *countingProcess) Handle(context.Context, in.ProcessOutbox) error {
	c.runs.Add(1)
	return nil
}

func TestRun_polls_until_the_context_ends(t *testing.T) {
	t.Parallel()
	process := &countingProcess{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		outbox.NewProcessor(process, 5*time.Millisecond).Run(ctx)
		close(done)
	}()
	assert.Eventually(t, func() bool { return process.runs.Load() >= 3 }, time.Second, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("processor did not stop")
	}
}
