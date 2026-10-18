package query

import "sync"

// StreamSlots enforces the per-user and global stream caps (DEC-18,
// ADR 0010 rule 15). Slots live in memory; a restart frees them all.
type StreamSlots struct {
	mu         sync.Mutex
	perUser    map[string]int
	total      int
	maxPerUser int
	maxTotal   int
}

func NewStreamSlots(maxPerUser, maxTotal int) *StreamSlots {
	if maxPerUser <= 0 || maxTotal <= 0 {
		panic("non-positive stream cap")
	}
	return &StreamSlots{perUser: map[string]int{}, maxPerUser: maxPerUser, maxTotal: maxTotal}
}

// acquire takes a slot for the user; release gives it back once.
func (s *StreamSlots) acquire(userID string) (release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.perUser[userID] >= s.maxPerUser:
		return nil, TooManyStreamsError{}
	case s.total >= s.maxTotal:
		return nil, TooManyStreamsError{Global: true}
	}
	s.perUser[userID]++
	s.total++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.total--
			if s.perUser[userID]--; s.perUser[userID] == 0 {
				delete(s.perUser, userID)
			}
		})
	}, nil
}
