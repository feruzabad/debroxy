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

	"github.com/feruzabad/debroxy/internal/common/id"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
	"github.com/feruzabad/debroxy/migrations"
)

// identityStorage is one implementation of every identity storage port.
type identityStorage struct {
	Name      string
	Users     out.UserRepository
	Sessions  out.SessionRepository
	ReadModel interface {
		out.AuthenticatedActorReadModel
		out.UsersReadModel
		out.UserDetailsReadModel
		out.UserStatusReadModel
	}
}

// newStorages returns fresh, empty storage for each implementation. Some
// rules count all users, so every test gets its own store.
func newStorages(t *testing.T) []identityStorage {
	t.Helper()
	database, err := commonsqlite.Open(context.Background(), commonsqlite.Config{Path: filepath.Join(t.TempDir(), "identity.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, commonsqlite.Migrate(context.Background(), database, migrations.Files))

	memoryUsers := memory.NewMemoryUserRepository()
	memorySessions := memory.NewMemorySessionRepository()
	return []identityStorage{
		{
			Name:      "memory",
			Users:     memoryUsers,
			Sessions:  memorySessions,
			ReadModel: memory.NewMemoryIdentityReadModel(memoryUsers, memorySessions),
		},
		{
			Name:      "sqlite",
			Users:     sqlite.NewSQLiteUserRepository(database),
			Sessions:  sqlite.NewSQLiteSessionRepository(database),
			ReadModel: sqlite.NewSQLiteIdentityReadModel(database),
		},
	}
}

func forEachStorage(t *testing.T, test func(t *testing.T, storage identityStorage)) {
	t.Helper()
	for _, storage := range newStorages(t) {
		t.Run(storage.Name, func(t *testing.T) {
			t.Parallel()
			test(t, storage)
		})
	}
}

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func newTestUser(t *testing.T, username string, role user.Role) *user.User {
	t.Helper()
	userID, err := user.NewID(id.New())
	require.NoError(t, err)
	name, err := user.NewUsername(username)
	require.NoError(t, err)
	hash, err := user.NewPasswordHash("$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5")
	require.NoError(t, err)
	u, err := user.NewUser(userID, name, role, hash, user.OIDCIdentity{}, now)
	require.NoError(t, err)
	return u
}

func addUser(t *testing.T, users out.UserRepository, u *user.User) {
	t.Helper()
	require.NoError(t, users.CreateUser(context.Background(), func(context.Context, out.UserPolicyReads) (*user.User, error) {
		return u, nil
	}))
}

func TestUserRepository_create_and_load(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleAdmin)
		addUser(t, storage.Users, alice)

		byID, err := storage.Users.UserByID(ctx, alice.ID())
		require.NoError(t, err)
		byName, err := storage.Users.UserByUsername(ctx, alice.Username())
		require.NoError(t, err)

		for _, loaded := range []*user.User{byID, byName} {
			assert.Equal(t, alice.ID(), loaded.ID())
			assert.Equal(t, alice.Username(), loaded.Username())
			assert.Equal(t, alice.Role(), loaded.Role())
			assert.Equal(t, user.StatusActive, loaded.Status())
			assert.Equal(t, alice.PasswordHash(), loaded.PasswordHash())
			assert.True(t, loaded.CreatedAt().Equal(now))
			assert.Empty(t, loaded.Events(), "loaded aggregates carry no events")
		}
	})
}

func TestUserRepository_rejects_a_taken_username(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		addUser(t, storage.Users, newTestUser(t, "alice", user.RoleUser))

		err := storage.Users.CreateUser(context.Background(), func(context.Context, out.UserPolicyReads) (*user.User, error) {
			return newTestUser(t, "alice", user.RoleUser), nil
		})

		var takenErr user.UsernameTakenError
		require.ErrorAs(t, err, &takenErr)
		assert.Equal(t, "alice", takenErr.Username)
	})
}

func TestUserRepository_create_function_error_stores_nothing(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		failure := errors.New("create function failed")

		err := storage.Users.CreateUser(context.Background(), func(context.Context, out.UserPolicyReads) (*user.User, error) {
			return nil, failure
		})

		require.ErrorIs(t, err, failure)
		users, err := storage.ReadModel.Users(context.Background())
		require.NoError(t, err)
		assert.Empty(t, users)
	})
}

func TestUserRepository_update_persists_changes(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		admin := newTestUser(t, "root", user.RoleAdmin)
		bob := newTestUser(t, "bob", user.RoleUser)
		addUser(t, storage.Users, admin)
		addUser(t, storage.Users, bob)

		err := storage.Users.UpdateUser(ctx, bob.ID(), func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error) {
			admins, err := reads.ActiveAdminCount(ctx)
			require.NoError(t, err)
			assert.Equal(t, 1, admins)
			count, err := reads.UserCount(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, count)
			require.NoError(t, u.Disable(admins))
			return u, nil
		})
		require.NoError(t, err)

		loaded, err := storage.Users.UserByID(ctx, bob.ID())
		require.NoError(t, err)
		assert.Equal(t, user.StatusDisabled, loaded.Status())
		assert.Equal(t, 1, loaded.SessionGeneration(), "disabling revokes sessions")
	})
}

func TestUserRepository_update_function_error_rolls_back(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		bob := newTestUser(t, "bob", user.RoleUser)
		addUser(t, storage.Users, bob)
		failure := errors.New("update function failed")

		err := storage.Users.UpdateUser(ctx, bob.ID(), func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
			require.NoError(t, u.ChangeRole(user.RoleAdmin, 0))
			return nil, failure
		})

		require.ErrorIs(t, err, failure)
		loaded, err := storage.Users.UserByID(ctx, bob.ID())
		require.NoError(t, err)
		assert.Equal(t, user.RoleUser, loaded.Role())
	})
}

func TestUserRepository_reports_unknown_users(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		unknown := newTestUser(t, "ghost", user.RoleUser)

		_, byIDErr := storage.Users.UserByID(ctx, unknown.ID())
		_, byNameErr := storage.Users.UserByUsername(ctx, unknown.Username())
		updateErr := storage.Users.UpdateUser(ctx, unknown.ID(), func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
			return u, nil
		})

		for _, err := range []error{byIDErr, byNameErr, updateErr} {
			assert.True(t, user.IsNotFound(err), "got %v", err)
		}
	})
}

// Two concurrent bootstraps on an empty store: exactly one admin results.
func TestUserRepository_parallel_creates_see_each_other(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		const attempts = 8
		results := make(chan error, attempts)
		var wg sync.WaitGroup
		for i := range attempts {
			wg.Go(func() {
				results <- storage.Users.CreateUser(context.Background(), func(ctx context.Context, reads out.UserPolicyReads) (*user.User, error) {
					count, err := reads.UserCount(ctx)
					if err != nil {
						return nil, err
					}
					if count > 0 {
						return nil, user.ErrBootstrapNotAllowed
					}
					return newTestUser(t, "admin"+string(rune('a'+i)), user.RoleAdmin), nil
				})
			})
		}
		wg.Wait()
		close(results)

		succeeded := 0
		for err := range results {
			if err == nil {
				succeeded++
				continue
			}
			assert.ErrorIs(t, err, user.ErrBootstrapNotAllowed)
		}
		assert.Equal(t, 1, succeeded)
	})
}

// Two admins disable each other at the same time: the last-admin rule must
// keep exactly one of them active (ASM-S01 for identity).
func TestUserRepository_parallel_updates_keep_the_last_admin(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		admins := []*user.User{newTestUser(t, "ann", user.RoleAdmin), newTestUser(t, "ben", user.RoleAdmin)}
		for _, admin := range admins {
			addUser(t, storage.Users, admin)
		}

		results := make(chan error, len(admins))
		var wg sync.WaitGroup
		for _, admin := range admins {
			wg.Go(func() {
				results <- storage.Users.UpdateUser(ctx, admin.ID(), func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error) {
					count, err := reads.ActiveAdminCount(ctx)
					if err != nil {
						return nil, err
					}
					if err := u.Disable(count); err != nil {
						return nil, err
					}
					return u, nil
				})
			})
		}
		wg.Wait()
		close(results)

		var lastAdminErrors, successes int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, user.ErrLastAdmin):
				lastAdminErrors++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}
		assert.Equal(t, 1, successes)
		assert.Equal(t, 1, lastAdminErrors)
	})
}

func newTestSession(t *testing.T, userID string, generation int) (*session.Session, string) {
	t.Helper()
	factory, err := session.NewFactory(session.FactoryConfig{AbsoluteLifetime: 720 * time.Hour, IdleTimeout: 168 * time.Hour})
	require.NoError(t, err)
	sessionID, err := session.NewID(id.New())
	require.NoError(t, err)
	token := "token-" + id.New() + "-0123456789"
	s, err := factory.NewSession(sessionID, userID, token, generation, now)
	require.NoError(t, err)
	return s, token
}

func TestSessionRepository_add_update_and_authenticate(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleAdmin)
		addUser(t, storage.Users, alice)
		s, token := newTestSession(t, alice.ID().String(), 0)
		require.NoError(t, storage.Sessions.AddSession(ctx, s))

		later := now.Add(10 * time.Minute)
		require.NoError(t, storage.Sessions.UpdateSession(ctx, s.ID(), func(_ context.Context, loaded *session.Session) (*session.Session, error) {
			loaded.Touch(later, time.Hour)
			return loaded, nil
		}))

		found, err := storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(token).String())
		require.NoError(t, err)
		assert.Equal(t, in.SessionForAuthentication{
			SessionID:             s.ID().String(),
			UserID:                alice.ID().String(),
			Role:                  "admin",
			UserStatus:            "active",
			UserSessionGeneration: 0,
			SessionGeneration:     0,
			LastSeenAt:            later,
			AbsoluteExpiresAt:     now.Add(720 * time.Hour),
			IdleExpiresAt:         later.Add(time.Hour),
		}, found)
	})
}

func TestSessionRepository_revocation_persists(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleUser)
		addUser(t, storage.Users, alice)
		s, token := newTestSession(t, alice.ID().String(), 0)
		require.NoError(t, storage.Sessions.AddSession(ctx, s))

		require.NoError(t, storage.Sessions.UpdateSession(ctx, s.ID(), func(_ context.Context, loaded *session.Session) (*session.Session, error) {
			loaded.Revoke(now.Add(time.Minute))
			return loaded, nil
		}))

		found, err := storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(token).String())
		require.NoError(t, err)
		assert.True(t, found.RevokedAt.Equal(now.Add(time.Minute)))
	})
}

func TestSessionRepository_update_function_error_rolls_back(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleUser)
		addUser(t, storage.Users, alice)
		s, token := newTestSession(t, alice.ID().String(), 0)
		require.NoError(t, storage.Sessions.AddSession(ctx, s))
		failure := errors.New("update function failed")

		err := storage.Sessions.UpdateSession(ctx, s.ID(), func(_ context.Context, loaded *session.Session) (*session.Session, error) {
			loaded.Revoke(now)
			return nil, failure
		})

		require.ErrorIs(t, err, failure)
		found, err := storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(token).String())
		require.NoError(t, err)
		assert.True(t, found.RevokedAt.IsZero())
	})
}

func TestSessionRepository_reports_unknown_sessions(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		s, token := newTestSession(t, id.New(), 0)

		updateErr := storage.Sessions.UpdateSession(ctx, s.ID(), func(_ context.Context, loaded *session.Session) (*session.Session, error) {
			return loaded, nil
		})
		_, lookupErr := storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(token).String())

		var notFound session.NotFoundError
		assert.ErrorAs(t, updateErr, &notFound)
		assert.ErrorIs(t, lookupErr, session.ErrSessionExpired)
	})
}

func TestIdentityReadModel_lists_and_describes_users(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		zoe := newTestUser(t, "zoe", user.RoleUser)
		amy := newTestUser(t, "amy", user.RoleAdmin)
		addUser(t, storage.Users, zoe)
		addUser(t, storage.Users, amy)

		users, err := storage.ReadModel.Users(ctx)
		require.NoError(t, err)
		details, err := storage.ReadModel.UserDetails(ctx, zoe.ID().String())
		require.NoError(t, err)
		_, unknownErr := storage.ReadModel.UserDetails(ctx, id.New())
		status, err := storage.ReadModel.UserStatus(ctx, zoe.ID().String())
		require.NoError(t, err)
		unknownStatus, err := storage.ReadModel.UserStatus(ctx, id.New())
		require.NoError(t, err)

		require.Len(t, users, 2)
		assert.Equal(t, []string{"amy", "zoe"}, []string{users[0].Username, users[1].Username})
		assert.Equal(t, in.UserSummary{
			ID: zoe.ID().String(), Username: "zoe", Role: "user", Status: "active",
			PasswordLogin: true, OIDCLinked: false, CreatedAt: now,
		}, details)
		assert.True(t, user.IsNotFound(unknownErr))
		assert.Equal(t, in.UserStatusResult{Exists: true, Active: true}, status)
		assert.Equal(t, in.UserStatusResult{}, unknownStatus)
	})
}

func TestSessionRepository_prunes_only_dead_sessions(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleUser)
		addUser(t, storage.Users, alice)
		live, liveToken := newTestSession(t, alice.ID().String(), 0)
		revoked, revokedToken := newTestSession(t, alice.ID().String(), 0)
		for _, s := range []*session.Session{live, revoked} {
			require.NoError(t, storage.Sessions.AddSession(ctx, s))
		}
		require.NoError(t, storage.Sessions.UpdateSession(ctx, revoked.ID(), func(_ context.Context, loaded *session.Session) (*session.Session, error) {
			loaded.Revoke(now)
			return loaded, nil
		}))

		pruned, err := storage.Sessions.PruneSessions(ctx, now.Add(time.Minute))
		require.NoError(t, err)
		assert.Equal(t, 1, pruned)
		_, err = storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(revokedToken).String())
		assert.Error(t, err, "revoked session pruned")
		_, err = storage.ReadModel.SessionForAuthentication(ctx, session.HashToken(liveToken).String())
		require.NoError(t, err)

		pruned, err = storage.Sessions.PruneSessions(ctx, live.IdleExpiresAt())
		require.NoError(t, err)
		assert.Equal(t, 1, pruned, "idle expiry ends the session")
	})
}

func TestUserRepository_finds_users_by_OIDC_identity_and_keeps_it_unique(t *testing.T) {
	t.Parallel()
	forEachStorage(t, func(t *testing.T, storage identityStorage) {
		ctx := context.Background()
		alice := newTestUser(t, "alice", user.RoleUser)
		bob := newTestUser(t, "bob", user.RoleUser)
		addUser(t, storage.Users, alice)
		addUser(t, storage.Users, bob)
		identity, err := user.NewOIDCIdentity("https://idp.example", "sub")
		require.NoError(t, err)
		link := func(u *user.User) error {
			return storage.Users.UpdateUser(ctx, u.ID(), func(_ context.Context, loaded *user.User, _ out.UserPolicyReads) (*user.User, error) {
				return loaded, loaded.LinkOIDCIdentity(identity)
			})
		}

		require.NoError(t, link(alice))
		found, err := storage.Users.UserByOIDCIdentity(ctx, identity)
		require.NoError(t, err)
		assert.Equal(t, alice.ID(), found.ID())

		assert.Equal(t, user.OIDCIdentityTakenError{}, link(bob), "the unique index is the backstop")

		other, err := user.NewOIDCIdentity("https://idp.example", "nobody")
		require.NoError(t, err)
		_, err = storage.Users.UserByOIDCIdentity(ctx, other)
		assert.True(t, user.IsNotFound(err))
	})
}
