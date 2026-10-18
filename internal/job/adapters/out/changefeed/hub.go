// Package changefeed is the in-memory Change Feed (ADR 0021 rule 6): it
// receives committed changes and hands them, as IDs only, to subscribers.
package changefeed

import (
	"sync"

	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// DefaultQueueSize bounds each subscriber's queue of undelivered notices (spec §5.11).
const DefaultQueueSize = 256

// Hub implements out.ChangeNotifier and out.ChangeSubscriptions. Notifying
// never blocks: a subscriber whose queue is full is overrun and dropped.
type Hub struct {
	queueSize int

	mu          sync.Mutex
	subscribers map[*subscription]struct{}
}

var (
	_ out.ChangeNotifier      = (*Hub)(nil)
	_ out.ChangeSubscriptions = (*Hub)(nil)
)

func NewHub(queueSize int) *Hub {
	if queueSize <= 0 {
		panic("non-positive queue size")
	}
	return &Hub{queueSize: queueSize, subscribers: map[*subscription]struct{}{}}
}

func (h *Hub) ItemChanged(change out.ItemChange) {
	h.publish(out.ChangeNotice{Item: &change})
}

func (h *Hub) ProviderStaleChanged(change out.ProviderStaleChange) {
	h.publish(out.ChangeNotice{ProviderStale: &change})
}

func (h *Hub) publish(notice out.ChangeNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subscribers {
		select {
		case s.notices <- notice:
		default:
			// The queue is full: the subscriber missed a notice, so it ends
			// and its client refetches (ADR 0021 rule 6).
			delete(h.subscribers, s)
			close(s.overrun)
		}
	}
}

func (h *Hub) Subscribe() out.ChangeSubscription {
	s := &subscription{hub: h, notices: make(chan out.ChangeNotice, h.queueSize), overrun: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[s] = struct{}{}
	return s
}

// Subscribers counts the current subscriptions.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

type subscription struct {
	hub     *Hub
	notices chan out.ChangeNotice
	overrun chan struct{}
}

func (s *subscription) Notices() <-chan out.ChangeNotice { return s.notices }
func (s *subscription) Overrun() <-chan struct{}         { return s.overrun }

func (s *subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	delete(s.hub.subscribers, s)
}
