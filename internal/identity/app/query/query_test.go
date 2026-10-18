package query_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/app/query"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

var start = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// fakeReadModel returns one fixed session for any token hash.
type fakeReadModel struct {
	session in.SessionForAuthentication
	found   bool
}

func (r fakeReadModel) SessionForAuthentication(context.Context, string) (in.SessionForAuthentication, error) {
	if !r.found {
		return in.SessionForAuthentication{}, session.ErrSessionExpired
	}
	return r.session, nil
}

func validSession() in.SessionForAuthentication {
	return in.SessionForAuthentication{
		SessionID: "s1", UserID: "u1", Role: "user", UserStatus: "active",
		UserSessionGeneration: 2, SessionGeneration: 2,
		LastSeenAt: start, AbsoluteExpiresAt: start.Add(24 * time.Hour), IdleExpiresAt: start.Add(time.Hour),
	}
}

func TestAuthenticatedActor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		modify   func(s *in.SessionForAuthentication)
		at       time.Time
		notFound bool
		want     in.AuthenticatedActorResult
		wantErr  error
	}{
		{"valid_session", func(*in.SessionForAuthentication) {}, start.Add(30 * time.Second), false,
			in.AuthenticatedActorResult{UserID: "u1", Role: "user", SessionID: "s1", SessionExpiresAt: start.Add(time.Hour), SessionIdleExpiresAt: start.Add(time.Hour), SessionAbsoluteExpiresAt: start.Add(24 * time.Hour), TouchDue: false}, nil},
		{"touch_due_after_a_minute", func(*in.SessionForAuthentication) {}, start.Add(time.Minute), false,
			in.AuthenticatedActorResult{UserID: "u1", Role: "user", SessionID: "s1", SessionExpiresAt: start.Add(time.Hour), SessionIdleExpiresAt: start.Add(61 * time.Minute), SessionAbsoluteExpiresAt: start.Add(24 * time.Hour), TouchDue: true}, nil},
		{"unknown_token", nil, start, true, in.AuthenticatedActorResult{}, session.ErrSessionExpired},
		{"idle_expired", func(*in.SessionForAuthentication) {}, start.Add(time.Hour), false, in.AuthenticatedActorResult{}, session.ErrSessionExpired},
		{"revoked", func(s *in.SessionForAuthentication) { s.RevokedAt = start }, start.Add(time.Second), false, in.AuthenticatedActorResult{}, session.ErrSessionExpired},
		{"user_disabled", func(s *in.SessionForAuthentication) { s.UserStatus = "disabled" }, start, false, in.AuthenticatedActorResult{}, session.ErrSessionExpired},
		{"sessions_revoked_by_generation", func(s *in.SessionForAuthentication) { s.UserSessionGeneration = 3 }, start, false, in.AuthenticatedActorResult{}, session.ErrSessionExpired},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := validSession()
			if tc.modify != nil {
				tc.modify(&found)
			}
			handler := query.NewAuthenticatedActorHandler(
				fakeReadModel{session: found, found: !tc.notFound}, time.Hour, clock.NewFixed(tc.at),
				slog.New(slog.DiscardHandler), decorator.NoOpMetricsClient{})

			result, err := handler.Handle(context.Background(), in.AuthenticatedActor{SessionToken: "token"})

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, result)
		})
	}
}
