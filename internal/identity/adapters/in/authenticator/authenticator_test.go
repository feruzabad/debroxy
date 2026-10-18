package authenticator_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/identity/adapters/in/authenticator"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

var idleExpiry = time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
var absoluteExpiry = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

type fakeQuery struct {
	result in.AuthenticatedActorResult
	err    error
}

func (f fakeQuery) Handle(context.Context, in.AuthenticatedActor) (in.AuthenticatedActorResult, error) {
	return f.result, f.err
}

type fakeCommand struct{ err error }

func (f fakeCommand) Handle(context.Context, in.TouchSession) error { return f.err }

func app(query in.AuthenticatedActorHandler, command in.TouchSessionHandler) in.Application {
	return in.Application{Queries: in.Queries{AuthenticatedActor: query}, Commands: in.Commands{TouchSession: command}}
}

func TestAuthenticator_fills_the_session_expiries(t *testing.T) {
	t.Parallel()
	query := fakeQuery{result: in.AuthenticatedActorResult{
		UserID: "u1", Role: "user", SessionID: "s1",
		SessionIdleExpiresAt: idleExpiry, SessionAbsoluteExpiresAt: absoluteExpiry,
	}}
	authenticator := authenticator.NewAuthenticator(app(query, fakeCommand{}), slog.New(slog.DiscardHandler))

	principal, err := authenticator.Authenticate(context.Background(), "token")

	require.NoError(t, err)
	assert.Equal(t, "u1", principal.UserID)
	assert.Equal(t, "user", principal.Role)
	assert.Equal(t, "s1", principal.SessionID)
	assert.Equal(t, idleExpiry, principal.SessionIdleExpiresAt)
	assert.Equal(t, absoluteExpiry, principal.SessionAbsoluteExpiresAt)
}

func TestAuthenticator_touches_only_when_due(t *testing.T) {
	t.Parallel()
	touched := 0
	command := &countingCommand{touched: &touched}
	query := fakeQuery{result: in.AuthenticatedActorResult{UserID: "u1", Role: "user", SessionID: "s1", TouchDue: true}}
	authenticator := authenticator.NewAuthenticator(app(query, command), slog.New(slog.DiscardHandler))

	_, err := authenticator.Authenticate(context.Background(), "token")

	require.NoError(t, err)
	assert.Equal(t, 1, *command.touched)
}

type countingCommand struct {
	touched *int
}

func (c *countingCommand) Handle(context.Context, in.TouchSession) error {
	*c.touched++
	return nil
}
