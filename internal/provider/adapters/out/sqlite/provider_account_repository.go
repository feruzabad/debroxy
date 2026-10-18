// Package sqlite persists the learned capability state of Provider
// Accounts (DEC-21, ADR 0003 rule 9). Accounts themselves come from
// configuration and are kept in memory.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
)

type SQLiteProviderAccountRepository struct {
	database *commonsqlite.Database
	mu       sync.Mutex
	order    []account.ID
	accounts map[account.ID]*account.ProviderAccount
}

// LoadCapabilityStates reads the stored states of one account, to build it
// at startup (stored rows override the initial states, ADR 0003 rule 8).
func LoadCapabilityStates(ctx context.Context, database *commonsqlite.Database, id account.ID) (map[account.Capability]account.CapabilityState, error) {
	rows, err := sqlcgen.New(database.Reader).CapabilityStatesOfProvider(ctx, id.String())
	if err != nil {
		return nil, fmt.Errorf("load capability states of %s: %w", id, err)
	}
	states := make(map[account.Capability]account.CapabilityState, len(rows))
	for _, row := range rows {
		capability, err := account.NewCapabilityFromString(row.Capability)
		if err != nil {
			continue // a capability removed from the code; the row is a stale cache entry
		}
		status, err := account.NewStatusFromString(row.State)
		if err != nil {
			return nil, err
		}
		var since time.Time
		if row.Since.Valid {
			since = time.UnixMilli(row.Since.Int64).UTC()
		}
		state, err := account.UnmarshalCapabilityStateFromDatabase(status, row.Reason.String, since)
		if err != nil {
			return nil, err
		}
		states[capability] = state
	}
	return states, nil
}

// NewSQLiteProviderAccountRepository keeps copies of accounts built with
// their stored states.
func NewSQLiteProviderAccountRepository(database *commonsqlite.Database, accounts []*account.ProviderAccount) *SQLiteProviderAccountRepository {
	if database == nil {
		panic("nil database")
	}
	r := &SQLiteProviderAccountRepository{database: database, accounts: map[account.ID]*account.ProviderAccount{}}
	for _, a := range accounts {
		r.order = append(r.order, a.ID())
		r.accounts[a.ID()] = a.Clone()
	}
	return r
}

func (r *SQLiteProviderAccountRepository) ProviderAccounts(context.Context) ([]*account.ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*account.ProviderAccount, 0, len(r.order))
	for _, id := range r.order {
		result = append(result, r.accounts[id].Clone())
	}
	return result, nil
}

func (r *SQLiteProviderAccountRepository) ProviderAccount(_ context.Context, id account.ID) (*account.ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok {
		return nil, account.NotFoundError{ID: id.String()}
	}
	return a.Clone(), nil
}

// UpdateProviderAccount stores the capability states updateFn changed in one
// write transaction, and keeps the new account in memory only after commit.
func (r *SQLiteProviderAccountRepository) UpdateProviderAccount(
	ctx context.Context, id account.ID, updateFn func(ctx context.Context, a *account.ProviderAccount) error,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.accounts[id]
	if !ok {
		return account.NotFoundError{ID: id.String()}
	}
	working := current.Clone()
	if err := updateFn(ctx, working); err != nil {
		return err
	}
	err := r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
		queries := sqlcgen.New(tx)
		for _, c := range account.AllCapabilities {
			before, _ := current.CapabilityState(c)
			after, supported := working.CapabilityState(c)
			if !supported || before == after {
				continue
			}
			if err := queries.UpsertCapabilityState(ctx, marshalCapabilityState(id, c, after)); err != nil {
				return fmt.Errorf("store capability state: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	r.accounts[id] = working
	return nil
}

func marshalCapabilityState(id account.ID, c account.Capability, state account.CapabilityState) sqlcgen.UpsertCapabilityStateParams {
	params := sqlcgen.UpsertCapabilityStateParams{
		ProviderID: id.String(),
		Capability: c.String(),
		State:      state.Status().String(),
		Reason:     sql.NullString{String: state.Reason(), Valid: state.Reason() != ""},
	}
	if !state.Since().IsZero() {
		params.Since = sql.NullInt64{Int64: state.Since().UnixMilli(), Valid: true}
	}
	return params
}
