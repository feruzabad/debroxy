// Package memory holds in-memory implementations of the identity driven
// ports, for tests and component tests (ddd-rules R-7, S-2). Each store
// holds one mutex for the whole load → reads → check → save section, so it
// races like SQLite's single writer (ADR 0002 rule 12).
package memory

import (
	"context"
	"sync"

	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type MemoryUserRepository struct {
	mu    sync.Mutex
	users map[user.ID]user.User
}

func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{users: map[user.ID]user.User{}}
}

func (r *MemoryUserRepository) CreateUser(
	ctx context.Context,
	createFn func(ctx context.Context, reads out.UserPolicyReads) (*user.User, error),
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, err := createFn(ctx, memoryUserPolicyReads{users: r.users})
	if err != nil {
		return err
	}
	for _, existing := range r.users {
		if existing.Username() == u.Username() || existing.ID() == u.ID() {
			return user.UsernameTakenError{Username: u.Username().String()}
		}
	}
	if err := r.checkOIDCIdentity(u); err != nil {
		return err
	}
	u.ClearEvents()
	r.users[u.ID()] = *u
	return nil
}

// checkOIDCIdentity emulates the unique index on (oidc_issuer, oidc_subject).
func (r *MemoryUserRepository) checkOIDCIdentity(u *user.User) error {
	if u.OIDCIdentity().IsZero() {
		return nil
	}
	for _, existing := range r.users {
		if existing.ID() != u.ID() && existing.OIDCIdentity() == u.OIDCIdentity() {
			return user.OIDCIdentityTakenError{}
		}
	}
	return nil
}

func (r *MemoryUserRepository) UpdateUser(
	ctx context.Context,
	id user.ID,
	updateFn func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error),
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.users[id]
	if !ok {
		return user.NotFoundError{ID: id.String()}
	}
	updated, err := updateFn(ctx, &stored, memoryUserPolicyReads{users: r.users})
	if err != nil {
		return err
	}
	if err := r.checkOIDCIdentity(updated); err != nil {
		return err
	}
	updated.ClearEvents()
	r.users[id] = *updated
	return nil
}

func (r *MemoryUserRepository) UserByID(_ context.Context, id user.ID) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.users[id]
	if !ok {
		return nil, user.NotFoundError{ID: id.String()}
	}
	return &stored, nil
}

func (r *MemoryUserRepository) UserByUsername(_ context.Context, username user.Username) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, stored := range r.users {
		if stored.Username() == username {
			return &stored, nil
		}
	}
	return nil, user.NotFoundError{ID: username.String()}
}

func (r *MemoryUserRepository) UserByOIDCIdentity(_ context.Context, identity user.OIDCIdentity) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, stored := range r.users {
		if !identity.IsZero() && stored.OIDCIdentity() == identity {
			return &stored, nil
		}
	}
	return nil, user.NotFoundError{ID: identity.Subject()}
}

// snapshot returns copies of all users for the read model.
func (r *MemoryUserRepository) snapshot() map[user.ID]user.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[user.ID]user.User, len(r.users))
	for id, u := range r.users {
		copied[id] = u
	}
	return copied
}

type memoryUserPolicyReads struct {
	users map[user.ID]user.User
}

func (r memoryUserPolicyReads) UserCount(context.Context) (int, error) {
	return len(r.users), nil
}

func (r memoryUserPolicyReads) ActiveAdminCount(context.Context) (int, error) {
	count := 0
	for _, u := range r.users {
		if u.IsActiveAdmin() {
			count++
		}
	}
	return count, nil
}

func (r memoryUserPolicyReads) OIDCIdentityHolder(_ context.Context, identity user.OIDCIdentity) (string, bool, error) {
	for _, u := range r.users {
		if !identity.IsZero() && u.OIDCIdentity() == identity {
			return u.ID().String(), true, nil
		}
	}
	return "", false, nil
}
