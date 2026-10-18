package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/identity/domain/session"
)

type MemorySessionRepository struct {
	mu       sync.Mutex
	sessions map[session.ID]session.Session
}

func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{sessions: map[session.ID]session.Session{}}
}

func (r *MemorySessionRepository) AddSession(_ context.Context, s *session.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.sessions {
		if existing.ID() == s.ID() || existing.TokenHash() == s.TokenHash() {
			return fmt.Errorf("session %s or its token already exists", s.ID())
		}
	}
	s.ClearEvents()
	r.sessions[s.ID()] = *s
	return nil
}

func (r *MemorySessionRepository) UpdateSession(
	ctx context.Context,
	id session.ID,
	updateFn func(ctx context.Context, s *session.Session) (*session.Session, error),
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.sessions[id]
	if !ok {
		return session.NotFoundError{ID: id.String()}
	}
	updated, err := updateFn(ctx, &stored)
	if err != nil {
		return err
	}
	updated.ClearEvents()
	r.sessions[id] = *updated
	return nil
}

func (r *MemorySessionRepository) sessionByTokenHash(tokenHash string) (session.Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.TokenHash().String() == tokenHash {
			return s, true
		}
	}
	return session.Session{}, false
}

func (r *MemorySessionRepository) PruneSessions(_ context.Context, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pruned := 0
	for id, s := range r.sessions {
		if !s.IsActiveAt(now) {
			delete(r.sessions, id)
			pruned++
		}
	}
	return pruned, nil
}
