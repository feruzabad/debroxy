package user_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/identity/domain/user"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func mustUser(t *testing.T, role user.Role) *user.User {
	t.Helper()
	id, err := user.NewID("01927f6e-0000-7000-8000-000000000001")
	require.NoError(t, err)
	username, err := user.NewUsername("alice")
	require.NoError(t, err)
	hash, err := user.NewPasswordHash("$argon2id$hash")
	require.NoError(t, err)
	u, err := user.NewUser(id, username, role, hash, user.OIDCIdentity{}, now)
	require.NoError(t, err)
	return u
}

func TestNewUsername(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		given   string
		want    string
		wantErr bool
	}{
		{"alice", "alice", false},
		{"Alice.Smith_2", "alice.smith_2", false},
		{"  bob  ", "bob", false},
		{"abc", "abc", false},
		{strings.Repeat("a", 32), strings.Repeat("a", 32), false},
		{"ab", "", true},
		{strings.Repeat("a", 33), "", true},
		{"al ice", "", true},
		{"alice@example", "", true},
		{"ålice", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.given, func(t *testing.T) {
			t.Parallel()

			username, err := user.NewUsername(tc.given)

			if tc.wantErr {
				assert.Equal(t, user.InvalidUsernameError{Given: tc.given}, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, username.String())
		})
	}
}

func TestNewPassword(t *testing.T) {
	t.Parallel()

	_, err := user.NewPassword("elevenchars")
	assert.Equal(t, user.PasswordTooShortError{MinimumLength: 12}, err)

	_, err = user.NewPassword(strings.Repeat("x", 1025))
	assert.Equal(t, user.PasswordTooLongError{MaximumBytes: 1024}, err)

	password, err := user.NewPassword("twelve chars")
	require.NoError(t, err)
	assert.Equal(t, "twelve chars", password.Plaintext())
	assert.Equal(t, "<redacted>", password.String())
}

func TestNewUser_needs_a_login_method(t *testing.T) {
	t.Parallel()
	id, err := user.NewID("01927f6e-0000-7000-8000-000000000001")
	require.NoError(t, err)
	username, err := user.NewUsername("alice")
	require.NoError(t, err)

	_, err = user.NewUser(id, username, user.RoleUser, user.PasswordHash{}, user.OIDCIdentity{}, now)

	assert.ErrorIs(t, err, user.ErrNoLoginMethod)
}

func TestNewUser_starts_active_and_records_creation(t *testing.T) {
	t.Parallel()

	u := mustUser(t, user.RoleUser)

	assert.True(t, u.IsActive())
	assert.True(t, u.CanLogInWithPassword())
	assert.Equal(t, 0, u.SessionGeneration())
	assert.Equal(t, []user.Event{user.UserCreated{UserID: u.ID(), Role: user.RoleUser}}, u.Events())
}

func TestUser_ChangeRole_last_admin_rule(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		role         user.Role
		disabled     bool
		newRole      user.Role
		activeAdmins int
		wantErr      error
	}{
		{"last_admin_cannot_be_demoted", user.RoleAdmin, false, user.RoleUser, 1, user.ErrLastAdmin},
		{"one_of_two_admins_can_be_demoted", user.RoleAdmin, false, user.RoleUser, 2, nil},
		{"disabled_admin_can_be_demoted", user.RoleAdmin, true, user.RoleUser, 1, nil},
		{"user_can_be_promoted", user.RoleUser, false, user.RoleAdmin, 1, nil},
		{"same_role_is_a_no_op", user.RoleAdmin, false, user.RoleAdmin, 1, nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := mustUser(t, tc.role)
			if tc.disabled {
				require.NoError(t, u.Disable(2))
			}

			err := u.ChangeRole(tc.newRole, tc.activeAdmins)

			assert.Equal(t, tc.wantErr, err)
			if tc.wantErr == nil {
				assert.Equal(t, tc.newRole, u.Role())
			} else {
				assert.Equal(t, tc.role, u.Role())
			}
		})
	}
}

func TestUser_Disable(t *testing.T) {
	t.Parallel()

	t.Run("last_admin_cannot_be_disabled", func(t *testing.T) {
		t.Parallel()
		u := mustUser(t, user.RoleAdmin)

		assert.ErrorIs(t, u.Disable(1), user.ErrLastAdmin)
		assert.True(t, u.IsActive())
	})

	t.Run("disabling_revokes_sessions_once", func(t *testing.T) {
		t.Parallel()
		u := mustUser(t, user.RoleUser)

		require.NoError(t, u.Disable(1))
		require.NoError(t, u.Disable(1))

		assert.Equal(t, user.StatusDisabled, u.Status())
		assert.False(t, u.CanLogInWithPassword())
		assert.Equal(t, 1, u.SessionGeneration())
	})

	t.Run("enable_keeps_sessions_revoked", func(t *testing.T) {
		t.Parallel()
		u := mustUser(t, user.RoleUser)
		require.NoError(t, u.Disable(1))

		u.Enable()

		assert.True(t, u.IsActive())
		assert.Equal(t, 1, u.SessionGeneration())
	})
}

func TestUser_ChangePassword_revokes_sessions(t *testing.T) {
	t.Parallel()
	u := mustUser(t, user.RoleUser)
	newHash, err := user.NewPasswordHash("$argon2id$new")
	require.NoError(t, err)

	require.NoError(t, u.ChangePassword(newHash))

	assert.Equal(t, newHash, u.PasswordHash())
	assert.Equal(t, 1, u.SessionGeneration())
	assert.Contains(t, u.Events(), user.Event(user.UserPasswordChanged{UserID: u.ID()}))
}

func TestPolicies(t *testing.T) {
	t.Parallel()
	alice, err := user.NewID("01927f6e-0000-7000-8000-000000000001")
	require.NoError(t, err)
	bob, err := user.NewID("01927f6e-0000-7000-8000-000000000002")
	require.NoError(t, err)
	admin := user.Actor{UserID: alice, Role: user.RoleAdmin}
	member := user.Actor{UserID: alice, Role: user.RoleUser}

	assert.NoError(t, user.CanManageUsers(admin))
	assert.ErrorIs(t, user.CanManageUsers(member), user.ErrForbidden)
	assert.NoError(t, user.CanSeeUser(admin, bob))
	assert.NoError(t, user.CanSeeUser(member, alice))
	assert.Equal(t, user.NotFoundError{ID: bob.String()}, user.CanSeeUser(member, bob))
}

func TestUser_OIDC_link_and_unlink(t *testing.T) {
	t.Parallel()
	u := mustUser(t, user.RoleUser)
	identity, err := user.NewOIDCIdentity("https://idp.example", "sub-1")
	require.NoError(t, err)
	assert.False(t, u.CanLogInWithOIDC())

	require.NoError(t, u.LinkOIDCIdentity(identity))
	assert.True(t, u.CanLogInWithOIDC())
	assert.Equal(t, identity, u.OIDCIdentity())
	assert.Contains(t, u.Events(), user.OIDCIdentityLinked{UserID: u.ID()})

	require.NoError(t, u.UnlinkOIDCIdentity())
	assert.True(t, u.OIDCIdentity().IsZero())
	assert.False(t, u.CanLogInWithOIDC())
}

func TestUser_unlink_keeps_the_only_login_method(t *testing.T) {
	t.Parallel()
	id, err := user.NewID("01927f6e-0000-7000-8000-000000000002")
	require.NoError(t, err)
	username, err := user.NewUsername("oidc-only")
	require.NoError(t, err)
	identity, err := user.NewOIDCIdentity("https://idp.example", "sub-2")
	require.NoError(t, err)
	u, err := user.NewUser(id, username, user.RoleUser, user.PasswordHash{}, identity, now)
	require.NoError(t, err)

	assert.Equal(t, user.ErrNoLoginMethod, u.UnlinkOIDCIdentity())
	assert.False(t, u.CanLogInWithPassword())

	require.NoError(t, u.Disable(2))
	assert.False(t, u.CanLogInWithOIDC(), "disabled users cannot log in")
}
