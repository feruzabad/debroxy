package memory

import (
	"context"
	"sort"

	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// MemoryIdentityReadModel answers the identity queries from the memory repositories.
type MemoryIdentityReadModel struct {
	users    *MemoryUserRepository
	sessions *MemorySessionRepository
}

func NewMemoryIdentityReadModel(users *MemoryUserRepository, sessions *MemorySessionRepository) MemoryIdentityReadModel {
	if users == nil {
		panic("nil users")
	}
	if sessions == nil {
		panic("nil sessions")
	}
	return MemoryIdentityReadModel{users: users, sessions: sessions}
}

func (r MemoryIdentityReadModel) SessionForAuthentication(_ context.Context, tokenHash string) (in.SessionForAuthentication, error) {
	s, ok := r.sessions.sessionByTokenHash(tokenHash)
	if !ok {
		return in.SessionForAuthentication{}, session.ErrSessionExpired
	}
	userID, err := user.NewID(s.UserID())
	if err != nil {
		return in.SessionForAuthentication{}, err
	}
	u, ok := r.users.snapshot()[userID]
	if !ok {
		return in.SessionForAuthentication{}, session.ErrSessionExpired
	}
	return in.SessionForAuthentication{
		SessionID:             s.ID().String(),
		UserID:                s.UserID(),
		Role:                  u.Role().String(),
		UserStatus:            u.Status().String(),
		UserSessionGeneration: u.SessionGeneration(),
		SessionGeneration:     s.UserSessionGeneration(),
		LastSeenAt:            s.LastSeenAt(),
		AbsoluteExpiresAt:     s.AbsoluteExpiresAt(),
		IdleExpiresAt:         s.IdleExpiresAt(),
		RevokedAt:             s.RevokedAt(),
	}, nil
}

func (r MemoryIdentityReadModel) Users(context.Context) ([]in.UserSummary, error) {
	summaries := []in.UserSummary{}
	for _, u := range r.users.snapshot() {
		summaries = append(summaries, userToSummary(u))
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Username < summaries[j].Username })
	return summaries, nil
}

func (r MemoryIdentityReadModel) UserDetails(_ context.Context, userID string) (in.UserSummary, error) {
	for id, u := range r.users.snapshot() {
		if id.String() == userID {
			return userToSummary(u), nil
		}
	}
	return in.UserSummary{}, user.NotFoundError{ID: userID}
}

func (r MemoryIdentityReadModel) UserStatus(_ context.Context, userID string) (in.UserStatusResult, error) {
	for id, u := range r.users.snapshot() {
		if id.String() == userID {
			return in.UserStatusResult{Exists: true, Active: u.IsActive()}, nil
		}
	}
	return in.UserStatusResult{}, nil
}

func userToSummary(u user.User) in.UserSummary {
	summary := in.UserSummary{
		ID:            u.ID().String(),
		Username:      u.Username().String(),
		Role:          u.Role().String(),
		Status:        u.Status().String(),
		PasswordLogin: !u.PasswordHash().IsZero(),
		OIDCLinked:    !u.OIDCIdentity().IsZero(),
		CreatedAt:     u.CreatedAt(),
	}
	if !u.OIDCIdentity().IsZero() {
		summary.OIDCIdentity = &in.OIDCIdentity{Issuer: u.OIDCIdentity().Issuer(), Subject: u.OIDCIdentity().Subject()}
	}
	return summary
}
