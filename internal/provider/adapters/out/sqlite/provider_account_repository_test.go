package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
	"github.com/feruzabad/debroxy/migrations"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

var torboxID = mustID("torbox")

func mustID(s string) account.ID {
	id, err := account.NewID(s)
	if err != nil {
		panic(err)
	}
	return id
}

func torboxAccount(t *testing.T, stored map[account.Capability]account.CapabilityState) *account.ProviderAccount {
	t.Helper()
	a, err := account.NewProviderAccount(account.Definition{
		ID: torboxID, Kind: account.KindTorBox, DisplayName: "TorBox", Enabled: true,
		KindCapabilities: []account.Capability{account.CapabilitySubmitMagnet, account.CapabilityCachedCheck, account.CapabilitySubmitNZBFile},
	}, stored)
	require.NoError(t, err)
	return a
}

func openDatabase(t *testing.T) *commonsqlite.Database {
	t.Helper()
	database, err := commonsqlite.Open(context.Background(), commonsqlite.Config{Path: filepath.Join(t.TempDir(), "provider.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, commonsqlite.Migrate(context.Background(), database, migrations.Files))
	return database
}

type repositoryCase struct {
	name       string
	repository out.ProviderAccountRepository
}

func repositories(t *testing.T) []repositoryCase {
	t.Helper()
	accounts := []*account.ProviderAccount{torboxAccount(t, nil)}
	return []repositoryCase{
		{"memory", memory.NewMemoryProviderAccountRepository(accounts)},
		{"sqlite", sqlite.NewSQLiteProviderAccountRepository(openDatabase(t), accounts)},
	}
}

func forEachRepository(t *testing.T, test func(t *testing.T, repository out.ProviderAccountRepository)) {
	t.Helper()
	for _, rc := range repositories(t) {
		t.Run(rc.name, func(t *testing.T) {
			t.Parallel()
			test(t, rc.repository)
		})
	}
}

func cachedCheckState(t *testing.T, repository out.ProviderAccountRepository) account.CapabilityState {
	t.Helper()
	a, err := repository.ProviderAccount(context.Background(), torboxID)
	require.NoError(t, err)
	state, ok := a.CapabilityState(account.CapabilityCachedCheck)
	require.True(t, ok)
	return state
}

func TestProviderAccountRepository_update_and_read(t *testing.T) {
	t.Parallel()
	forEachRepository(t, func(t *testing.T, repository out.ProviderAccountRepository) {
		ctx := context.Background()

		require.NoError(t, repository.UpdateProviderAccount(ctx, torboxID, func(_ context.Context, a *account.ProviderAccount) error {
			a.RecordPlanRestriction(account.CapabilityCachedCheck, now)
			return nil
		}))

		assert.Equal(t, account.Unavailable(account.ReasonPlan, now), cachedCheckState(t, repository))
		all, err := repository.ProviderAccounts(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, "TorBox", all[0].DisplayName())
	})
}

func TestProviderAccountRepository_update_function_error_changes_nothing(t *testing.T) {
	t.Parallel()
	forEachRepository(t, func(t *testing.T, repository out.ProviderAccountRepository) {
		failure := errors.New("update function failed")

		err := repository.UpdateProviderAccount(context.Background(), torboxID, func(_ context.Context, a *account.ProviderAccount) error {
			a.RecordPlanRestriction(account.CapabilityCachedCheck, now)
			return failure
		})

		require.ErrorIs(t, err, failure)
		assert.Equal(t, account.StatusAvailable, cachedCheckState(t, repository).Status())
	})
}

func TestProviderAccountRepository_returns_copies(t *testing.T) {
	t.Parallel()
	forEachRepository(t, func(t *testing.T, repository out.ProviderAccountRepository) {
		a, err := repository.ProviderAccount(context.Background(), torboxID)
		require.NoError(t, err)

		a.RecordPlanRestriction(account.CapabilityCachedCheck, now)

		assert.Equal(t, account.StatusAvailable, cachedCheckState(t, repository).Status(), "changes outside UpdateProviderAccount are not stored")
	})
}

func TestProviderAccountRepository_reports_unknown_accounts(t *testing.T) {
	t.Parallel()
	forEachRepository(t, func(t *testing.T, repository out.ProviderAccountRepository) {
		ctx := context.Background()
		unknown := mustID("premiumize")

		_, readErr := repository.ProviderAccount(ctx, unknown)
		updateErr := repository.UpdateProviderAccount(ctx, unknown, func(context.Context, *account.ProviderAccount) error { return nil })

		assert.Equal(t, account.NotFoundError{ID: "premiumize"}, readErr)
		assert.Equal(t, account.NotFoundError{ID: "premiumize"}, updateErr)
	})
}

func TestProviderAccountRepository_parallel_updates_are_serialized(t *testing.T) {
	t.Parallel()
	forEachRepository(t, func(t *testing.T, repository out.ProviderAccountRepository) {
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Go(func() {
				err := repository.UpdateProviderAccount(context.Background(), torboxID, func(_ context.Context, a *account.ProviderAccount) error {
					if i%2 == 0 {
						a.RecordPlanRestriction(account.CapabilityCachedCheck, now)
					} else {
						a.RecordCapabilitySuccess(account.CapabilityCachedCheck, now)
					}
					return nil
				})
				assert.NoError(t, err)
			})
		}
		wg.Wait()

		state := cachedCheckState(t, repository).Status()
		assert.Contains(t, []account.Status{account.StatusAvailable, account.StatusUnavailable}, state)
	})
}

func TestSQLiteProviderAccountRepository_states_survive_a_restart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openDatabase(t)
	first := sqlite.NewSQLiteProviderAccountRepository(database, []*account.ProviderAccount{torboxAccount(t, nil)})
	require.NoError(t, first.UpdateProviderAccount(ctx, torboxID, func(_ context.Context, a *account.ProviderAccount) error {
		a.RecordPlanRestriction(account.CapabilityCachedCheck, now)
		a.RecordCapabilitySuccess(account.CapabilitySubmitNZBFile, now)
		return nil
	}))

	stored, err := sqlite.LoadCapabilityStates(ctx, database, torboxID)
	require.NoError(t, err)
	restarted := sqlite.NewSQLiteProviderAccountRepository(database, []*account.ProviderAccount{torboxAccount(t, stored)})

	assert.Equal(t, map[account.Capability]account.CapabilityState{
		account.CapabilityCachedCheck:   account.Unavailable(account.ReasonPlan, now),
		account.CapabilitySubmitNZBFile: account.Available(now),
	}, stored, "only changed states are stored")
	assert.Equal(t, account.Unavailable(account.ReasonPlan, now), cachedCheckState(t, restarted))
}
