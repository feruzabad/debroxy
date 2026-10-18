package command

import (
	"context"
	"sync"
)

// Background runs work that outlives the request or tick that started it:
// a submission past the request deadline (ADR 0005 rule 6), a coalesced
// webhook refresh, the scheduler. Its context is the process lifetime;
// Wait lets shutdown finish the work before the database closes (NFR-06).
type Background struct {
	ctx    context.Context
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func NewBackground(ctx context.Context) *Background {
	if ctx == nil {
		panic("nil ctx")
	}
	return &Background{ctx: ctx}
}

// Go runs fn with the process context. After Wait has begun it runs
// nothing and reports false.
func (b *Background) Go(fn func(ctx context.Context)) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		fn(b.ctx)
	}()
	return true
}

// Wait stops accepting work and blocks until every started function returned.
func (b *Background) Wait() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.wg.Wait()
}
