package command_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/identity/app/command"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

var start = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// fakeHasher stores "hash:<password>" and counts verifications.
type fakeHasher struct {
	verifications int
}

func (h *fakeHasher) Hash(password user.Password) (user.PasswordHash, error) {
	return user.NewPasswordHash("hash:" + password.Plaintext())
}

func (h *fakeHasher) Verify(hash user.PasswordHash, password string) (bool, error) {
	h.verifications++
	return hash.String() == "hash:"+password, nil
}

// fakeLimiter blocks when blocked is set and records calls.
type fakeLimiter struct {
	blocked   time.Duration
	failures  []string
	successes []string
}

func (l *fakeLimiter) RetryAfter(string, string, time.Time) time.Duration { return l.blocked }
func (l *fakeLimiter) RecordFailure(username, _ string, _ time.Time) {
	l.failures = append(l.failures, username)
}
func (l *fakeLimiter) RecordSuccess(username, _ string) { l.successes = append(l.successes, username) }

type fixture struct {
	users     *memory.MemoryUserRepository
	sessions  *memory.MemorySessionRepository
	readModel memory.MemoryIdentityReadModel
	hasher    *fakeHasher
	limiter   *fakeLimiter
	clock     *clock.Fixed
	factory   session.Factory
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	factory, err := session.NewFactory(session.FactoryConfig{AbsoluteLifetime: 24 * time.Hour, IdleTimeout: time.Hour})
	require.NoError(t, err)
	users := memory.NewMemoryUserRepository()
	sessions := memory.NewMemorySessionRepository()
	return &fixture{
		users: users, sessions: sessions, readModel: memory.NewMemoryIdentityReadModel(users, sessions),
		hasher: &fakeHasher{}, limiter: &fakeLimiter{}, clock: clock.NewFixed(start), factory: factory,
	}
}

var (
	logger  = slog.New(slog.DiscardHandler)
	metrics = decorator.NoOpMetricsClient{}
)

func (f *fixture) addUser(t *testing.T, username, password string, role user.Role) *user.User {
	t.Helper()
	userID, err := user.NewID(id.New())
	require.NoError(t, err)
	name, err := user.NewUsername(username)
	require.NoError(t, err)
	hash, err := user.NewPasswordHash("hash:" + password)
	require.NoError(t, err)
	u, err := user.NewUser(userID, name, role, hash, user.OIDCIdentity{}, start)
	require.NoError(t, err)
	require.NoError(t, f.users.CreateUser(context.Background(), func(context.Context, out.UserPolicyReads) (*user.User, error) {
		return u, nil
	}))
	return u
}

func (f *fixture) logIn(t *testing.T, username, password string) (in.LogInWithPassword, error) {
	t.Helper()
	handler, err := command.NewLogInWithPasswordHandler(f.users, f.sessions, f.hasher, f.limiter, f.factory, f.clock, logger, metrics)
	require.NoError(t, err)
	cmd := in.LogInWithPassword{
		Username: username, Password: password, ClientIP: "203.0.113.7",
		SessionID: id.New(), SessionToken: strings.Repeat("t", 43) + id.New(),
	}
	return cmd, handler.Handle(context.Background(), cmd)
}

func (f *fixture) sessionOf(t *testing.T, token string) in.SessionForAuthentication {
	t.Helper()
	found, err := f.readModel.SessionForAuthentication(context.Background(), session.HashToken(token).String())
	require.NoError(t, err)
	return found
}

func TestLogInWithPassword_starts_a_session(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)

	cmd, err := f.logIn(t, "Alice", "correct horse battery")

	require.NoError(t, err)
	found := f.sessionOf(t, cmd.SessionToken)
	assert.Equal(t, cmd.SessionID, found.SessionID)
	assert.Equal(t, alice.ID().String(), found.UserID)
	assert.Equal(t, start.Add(time.Hour), found.IdleExpiresAt)
	assert.Equal(t, []string{"Alice"}, f.limiter.successes)
}

func TestLogInWithPassword_failures_look_the_same(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		username string
		password string
		disable  bool
	}{
		{"wrong_password", "alice", "wrong password value", false},
		{"unknown_user", "mallory", "correct horse battery", false},
		{"malformed_username", "x", "correct horse battery", false},
		{"disabled_user", "alice", "correct horse battery", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.addUser(t, "root", "correct horse battery", user.RoleAdmin)
			alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
			if tc.disable {
				require.NoError(t, f.users.UpdateUser(context.Background(), alice.ID(),
					func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
						return u, u.Disable(1)
					}))
			}

			_, err := f.logIn(t, tc.username, tc.password)

			assert.ErrorIs(t, err, user.ErrInvalidCredentials)
			assert.Equal(t, 1, f.hasher.verifications, "every failure verifies one hash, so timing does not tell them apart")
			assert.Equal(t, []string{tc.username}, f.limiter.failures)
		})
	}
}

func TestLogInWithPassword_throttled_attempts_check_no_password(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	f.limiter.blocked = 90 * time.Second

	_, err := f.logIn(t, "alice", "correct horse battery")

	assert.Equal(t, command.LoginThrottledError{Wait: 90 * time.Second}, err)
	assert.Zero(t, f.hasher.verifications, "a throttled attempt checks no password")
}

func TestBootstrapAdmin_only_on_an_empty_store(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	handler := command.NewBootstrapAdminHandler(f.users, f.hasher, f.clock, logger, metrics)
	cmd := in.BootstrapAdmin{UserID: id.New(), Username: "root", Password: "correct horse battery"}

	require.NoError(t, handler.Handle(context.Background(), cmd))
	err := handler.Handle(context.Background(), in.BootstrapAdmin{UserID: id.New(), Username: "second", Password: "correct horse battery"})

	assert.ErrorIs(t, err, user.ErrBootstrapNotAllowed)
	details, err := f.readModel.UserDetails(context.Background(), cmd.UserID)
	require.NoError(t, err)
	assert.Equal(t, "admin", details.Role)
}

func TestCreateUser_needs_an_admin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	member := f.addUser(t, "member", "correct horse battery", user.RoleUser)
	handler := command.NewCreateUserHandler(f.users, f.hasher, f.clock, logger, metrics)

	err := handler.Handle(context.Background(), in.CreateUser{
		Actor:  in.Actor{UserID: member.ID().String(), Role: "user"},
		UserID: id.New(), Username: "newbie", Role: "user", Password: "correct horse battery",
	})

	assert.ErrorIs(t, err, user.ErrForbidden)
}

func TestChangeOwnPassword_keeps_only_the_current_session(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	current, err := f.logIn(t, "alice", "correct horse battery")
	require.NoError(t, err)
	other, err := f.logIn(t, "alice", "correct horse battery")
	require.NoError(t, err)
	handler := command.NewChangeOwnPasswordHandler(f.users, f.sessions, f.hasher, logger, metrics)

	err = handler.Handle(context.Background(), in.ChangeOwnPassword{
		Actor:           in.Actor{UserID: alice.ID().String(), Role: "user", SessionID: current.SessionID},
		CurrentPassword: "correct horse battery",
		NewPassword:     "a much better passphrase",
	})

	require.NoError(t, err)
	currentSession, otherSession := f.sessionOf(t, current.SessionToken), f.sessionOf(t, other.SessionToken)
	assert.Equal(t, 1, currentSession.UserSessionGeneration)
	assert.Equal(t, 1, currentSession.SessionGeneration, "the current session follows the new generation")
	assert.Equal(t, 0, otherSession.SessionGeneration, "other sessions are revoked")
	_, err = f.logIn(t, "alice", "a much better passphrase")
	assert.NoError(t, err)
}

func TestChangeOwnPassword_rejects_a_wrong_current_password(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	current, err := f.logIn(t, "alice", "correct horse battery")
	require.NoError(t, err)
	handler := command.NewChangeOwnPasswordHandler(f.users, f.sessions, f.hasher, logger, metrics)

	err = handler.Handle(context.Background(), in.ChangeOwnPassword{
		Actor:           in.Actor{UserID: alice.ID().String(), Role: "user", SessionID: current.SessionID},
		CurrentPassword: "not my password at all",
		NewPassword:     "a much better passphrase",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "current password is wrong")
	assert.Equal(t, 0, f.sessionOf(t, current.SessionToken).UserSessionGeneration)
}

func TestLogOut_revokes_only_the_actors_own_session(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	bob := f.addUser(t, "bob", "correct horse battery", user.RoleUser)
	aliceLogin, err := f.logIn(t, "alice", "correct horse battery")
	require.NoError(t, err)
	handler := command.NewLogOutHandler(f.sessions, f.clock, logger, metrics)

	bobErr := handler.Handle(context.Background(), in.LogOut{Actor: in.Actor{UserID: bob.ID().String(), SessionID: aliceLogin.SessionID}})
	aliceErr := handler.Handle(context.Background(), in.LogOut{Actor: in.Actor{UserID: alice.ID().String(), SessionID: aliceLogin.SessionID}})

	var notFound session.NotFoundError
	assert.ErrorAs(t, bobErr, &notFound)
	require.NoError(t, aliceErr)
	assert.Equal(t, start, f.sessionOf(t, aliceLogin.SessionToken).RevokedAt)
}

func (f *fixture) linkOIDC(t *testing.T, u *user.User, subject string) {
	t.Helper()
	admin := in.Actor{UserID: id.New(), Role: "admin"}
	err := command.NewLinkOIDCIdentityHandler(f.users, logger, metrics).Handle(context.Background(),
		in.LinkOIDCIdentity{Actor: admin, UserID: u.ID().String(), Issuer: "https://idp.example", Subject: subject})
	require.NoError(t, err)
}

func (f *fixture) logInWithOIDC(t *testing.T, subject string) (in.LogInWithOIDC, error) {
	t.Helper()
	cmd := in.LogInWithOIDC{Issuer: "https://idp.example", Subject: subject, SessionID: id.New(), SessionToken: strings.Repeat("o", 43) + id.New()}
	return cmd, command.NewLogInWithOIDCHandler(f.users, f.sessions, f.factory, f.clock, logger, metrics).Handle(context.Background(), cmd)
}

func TestLogInWithOIDC_logs_in_only_linked_active_users(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	f.linkOIDC(t, alice, "alice-sub")

	cmd, err := f.logInWithOIDC(t, "alice-sub")
	require.NoError(t, err)
	assert.Equal(t, alice.ID().String(), f.sessionOf(t, cmd.SessionToken).UserID)

	_, err = f.logInWithOIDC(t, "stranger-sub")
	assert.Equal(t, user.ErrInvalidCredentials, err, "no just-in-time provisioning (DEC-04)")

	require.NoError(t, f.users.UpdateUser(context.Background(), alice.ID(), func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
		return u, u.Disable(2)
	}))
	_, err = f.logInWithOIDC(t, "alice-sub")
	assert.Equal(t, user.ErrInvalidCredentials, err)
}

func TestLinkOIDCIdentity_refuses_an_identity_of_another_user(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	alice := f.addUser(t, "alice", "correct horse battery", user.RoleUser)
	bob := f.addUser(t, "bob", "correct horse battery", user.RoleUser)
	f.linkOIDC(t, alice, "shared-sub")
	admin := in.Actor{UserID: id.New(), Role: "admin"}
	link := command.NewLinkOIDCIdentityHandler(f.users, logger, metrics)

	err := link.Handle(context.Background(), in.LinkOIDCIdentity{Actor: admin, UserID: bob.ID().String(), Issuer: "https://idp.example", Subject: "shared-sub"})
	assert.Equal(t, user.OIDCIdentityTakenError{}, err)

	err = link.Handle(context.Background(), in.LinkOIDCIdentity{Actor: in.Actor{UserID: bob.ID().String(), Role: "user"}, UserID: bob.ID().String(), Issuer: "https://idp.example", Subject: "b"})
	assert.Equal(t, user.ErrForbidden, err)
}

func TestCreateUser_with_only_an_OIDC_identity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	admin := in.Actor{UserID: id.New(), Role: "admin"}
	create := command.NewCreateUserHandler(f.users, f.hasher, f.clock, logger, metrics)
	userID := id.New()

	require.NoError(t, create.Handle(context.Background(), in.CreateUser{
		Actor: admin, UserID: userID, Username: "carol", Role: "user", OIDCIssuer: "https://idp.example", OIDCSubject: "carol-sub",
	}))
	_, err := f.logInWithOIDC(t, "carol-sub")
	require.NoError(t, err)

	err = create.Handle(context.Background(), in.CreateUser{Actor: admin, UserID: id.New(), Username: "dave", Role: "user"})
	assert.Equal(t, user.ErrNoLoginMethod, err)
	err = create.Handle(context.Background(), in.CreateUser{
		Actor: admin, UserID: id.New(), Username: "erin", Role: "user", OIDCIssuer: "https://idp.example", OIDCSubject: "carol-sub",
	})
	assert.Equal(t, user.OIDCIdentityTakenError{}, err)
}
