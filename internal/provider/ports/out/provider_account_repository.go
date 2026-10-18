package out

import (
	"context"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
)

// ProviderAccountRepository holds the Provider Accounts. They come from
// configuration and live in memory; only their capability states are
// persisted (DEC-21, ADR 0003 rule 9).
type ProviderAccountRepository interface {
	// ProviderAccounts returns copies of all accounts in configuration order.
	ProviderAccounts(ctx context.Context) ([]*account.ProviderAccount, error)
	// ProviderAccount returns a copy; an unknown ID is account.NotFoundError.
	ProviderAccount(ctx context.Context, id account.ID) (*account.ProviderAccount, error)
	// UpdateProviderAccount applies updateFn to the account and stores the
	// capability states it changed, in one transaction (R-2).
	UpdateProviderAccount(ctx context.Context, id account.ID, updateFn func(ctx context.Context, a *account.ProviderAccount) error) error
}
