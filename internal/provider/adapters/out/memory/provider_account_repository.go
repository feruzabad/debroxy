// Package memory holds the in-memory ProviderAccountRepository, for tests
// and component tests (ddd-rules R-7).
package memory

import (
	"context"
	"sync"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
)

type MemoryProviderAccountRepository struct {
	mu       sync.Mutex
	order    []account.ID
	accounts map[account.ID]*account.ProviderAccount
}

// NewMemoryProviderAccountRepository keeps copies of accounts, in this order.
func NewMemoryProviderAccountRepository(accounts []*account.ProviderAccount) *MemoryProviderAccountRepository {
	r := &MemoryProviderAccountRepository{accounts: map[account.ID]*account.ProviderAccount{}}
	for _, a := range accounts {
		r.order = append(r.order, a.ID())
		r.accounts[a.ID()] = a.Clone()
	}
	return r
}

func (r *MemoryProviderAccountRepository) ProviderAccounts(context.Context) ([]*account.ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*account.ProviderAccount, 0, len(r.order))
	for _, id := range r.order {
		result = append(result, r.accounts[id].Clone())
	}
	return result, nil
}

func (r *MemoryProviderAccountRepository) ProviderAccount(_ context.Context, id account.ID) (*account.ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok {
		return nil, account.NotFoundError{ID: id.String()}
	}
	return a.Clone(), nil
}

func (r *MemoryProviderAccountRepository) UpdateProviderAccount(
	ctx context.Context, id account.ID, updateFn func(ctx context.Context, a *account.ProviderAccount) error,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok {
		return account.NotFoundError{ID: id.String()}
	}
	working := a.Clone()
	if err := updateFn(ctx, working); err != nil {
		return err
	}
	r.accounts[id] = working
	return nil
}
