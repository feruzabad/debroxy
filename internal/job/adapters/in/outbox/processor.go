// Package outbox is the outbox processor: the driving adapter that runs
// ProcessOutbox, so recorded side effects reach the provider (ADR 0008).
package outbox

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// Processor polls the outbox. The first retry is due 1 s after a failure,
// so it polls every second.
type Processor struct {
	process  in.ProcessOutboxHandler
	interval time.Duration
}

func NewProcessor(process in.ProcessOutboxHandler, interval time.Duration) Processor {
	if process == nil {
		panic("nil processOutbox")
	}
	if interval <= 0 {
		panic("non-positive interval")
	}
	return Processor{process: process, interval: interval}
}

// Run polls until ctx ends; failures are logged by the decorator.
func (p Processor) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		_ = p.process.Handle(ctx, in.ProcessOutbox{})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
