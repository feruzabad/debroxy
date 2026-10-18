package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/identity/domain/session"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

const token = "dG9rZW4tdG9rZW4tdG9rZW4tdG9rZW4tdG9rZW4tdG9rZW4"

func mustFactory(t *testing.T, absolute, idle time.Duration) session.Factory {
	t.Helper()
	factory, err := session.NewFactory(session.FactoryConfig{AbsoluteLifetime: absolute, IdleTimeout: idle})
	require.NoError(t, err)
	return factory
}

func mustSession(t *testing.T, factory session.Factory) *session.Session {
	t.Helper()
	id, err := session.NewID("01927f6e-0000-7000-8000-0000000000aa")
	require.NoError(t, err)
	s, err := factory.NewSession(id, "user-1", token, 3, now)
	require.NoError(t, err)
	return s
}

func TestNewFactory_validates_lifetimes(t *testing.T) {
	t.Parallel()

	_, err := session.NewFactory(session.FactoryConfig{AbsoluteLifetime: time.Hour})
	assert.Error(t, err)
	_, err = session.NewFactory(session.FactoryConfig{AbsoluteLifetime: time.Hour, IdleTimeout: 2 * time.Hour})
	assert.Error(t, err)
}

func TestFactory_NewSession(t *testing.T) {
	t.Parallel()
	s := mustSession(t, mustFactory(t, 720*time.Hour, 168*time.Hour))

	assert.Equal(t, session.HashToken(token), s.TokenHash())
	assert.NotContains(t, s.TokenHash().String(), token)
	assert.Equal(t, 3, s.UserSessionGeneration())
	assert.Equal(t, now.Add(720*time.Hour), s.AbsoluteExpiresAt())
	assert.Equal(t, now.Add(168*time.Hour), s.IdleExpiresAt())
	assert.True(t, s.IsActiveAt(now))
}

func TestFactory_NewSession_rejects_short_tokens(t *testing.T) {
	t.Parallel()
	id, err := session.NewID("01927f6e-0000-7000-8000-0000000000aa")
	require.NoError(t, err)

	_, err = mustFactory(t, time.Hour, time.Hour).NewSession(id, "user-1", "short", 0, now)

	assert.Error(t, err)
}

func TestSession_IsActiveAt(t *testing.T) {
	t.Parallel()
	factory := mustFactory(t, 10*time.Hour, 2*time.Hour)

	testCases := []struct {
		name    string
		prepare func(s *session.Session)
		at      time.Time
		want    bool
	}{
		{"fresh", func(*session.Session) {}, now.Add(time.Hour), true},
		{"idle_timeout_reached", func(*session.Session) {}, now.Add(2 * time.Hour), false},
		{"touched_extends_idle", func(s *session.Session) { s.Touch(now.Add(90*time.Minute), 2*time.Hour) }, now.Add(3 * time.Hour), true},
		{"absolute_lifetime_wins", func(s *session.Session) {
			for h := 1; h <= 9; h++ {
				s.Touch(now.Add(time.Duration(h)*time.Hour), 2*time.Hour)
			}
		}, now.Add(10 * time.Hour), false},
		{"revoked", func(s *session.Session) { s.Revoke(now.Add(time.Minute)) }, now.Add(2 * time.Minute), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := mustSession(t, factory)
			tc.prepare(s)

			assert.Equal(t, tc.want, s.IsActiveAt(tc.at))
		})
	}
}

func TestSession_Touch(t *testing.T) {
	t.Parallel()

	t.Run("at_most_once_per_interval", func(t *testing.T) {
		t.Parallel()
		s := mustSession(t, mustFactory(t, 10*time.Hour, 2*time.Hour))

		s.Touch(now.Add(30*time.Second), 2*time.Hour)
		assert.Equal(t, now, s.LastSeenAt())

		s.Touch(now.Add(session.TouchInterval), 2*time.Hour)
		assert.Equal(t, now.Add(session.TouchInterval), s.LastSeenAt())
	})

	t.Run("never_past_the_absolute_expiry", func(t *testing.T) {
		t.Parallel()
		s := mustSession(t, mustFactory(t, 3*time.Hour, 2*time.Hour))

		s.Touch(now.Add(2*time.Hour-time.Minute), 2*time.Hour)

		assert.Equal(t, now.Add(3*time.Hour), s.IdleExpiresAt())
		assert.Equal(t, now.Add(3*time.Hour), session.ExpiresAt(s.AbsoluteExpiresAt(), s.IdleExpiresAt()))
	})

	t.Run("does_not_revive_an_expired_session", func(t *testing.T) {
		t.Parallel()
		s := mustSession(t, mustFactory(t, 10*time.Hour, time.Hour))

		s.Touch(now.Add(2*time.Hour), time.Hour)

		assert.False(t, s.IsActiveAt(now.Add(2*time.Hour)))
	})
}

func TestSession_Revoke_is_idempotent(t *testing.T) {
	t.Parallel()
	s := mustSession(t, mustFactory(t, time.Hour, time.Hour))
	s.ClearEvents()

	s.Revoke(now.Add(time.Minute))
	s.Revoke(now.Add(2 * time.Minute))

	assert.Equal(t, now.Add(time.Minute), s.RevokedAt())
	assert.Len(t, s.Events(), 1)
}

func TestSession_FollowUserSessionGeneration(t *testing.T) {
	t.Parallel()
	s := mustSession(t, mustFactory(t, time.Hour, time.Hour))

	require.NoError(t, s.FollowUserSessionGeneration(4))
	assert.Equal(t, 4, s.UserSessionGeneration())
	assert.Error(t, s.FollowUserSessionGeneration(2), "generations never move backwards")
}
