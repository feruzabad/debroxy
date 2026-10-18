package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type logInWithPasswordHandler struct {
	users     out.UserRepository
	sessions  out.SessionRepository
	hasher    out.PasswordHasher
	limiter   out.LoginAttemptLimiter
	factory   session.Factory
	clock     clock.Clock
	decoyHash user.PasswordHash
}

// NewLogInWithPasswordHandler hashes a decoy password once at startup. Login
// attempts for unknown users verify against it, so they take as long as
// attempts for real users and do not reveal which usernames exist.
func NewLogInWithPasswordHandler(
	users out.UserRepository, sessions out.SessionRepository, hasher out.PasswordHasher,
	limiter out.LoginAttemptLimiter, factory session.Factory, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) (in.LogInWithPasswordHandler, error) {
	switch {
	case users == nil:
		panic("nil users")
	case sessions == nil:
		panic("nil sessions")
	case hasher == nil:
		panic("nil hasher")
	case limiter == nil:
		panic("nil limiter")
	case factory.IsZero():
		panic("zero session factory")
	case clock == nil:
		panic("nil clock")
	}
	decoyPassword, err := user.NewPassword("decoy password for unknown users")
	if err != nil {
		return nil, err
	}
	decoyHash, err := hasher.Hash(decoyPassword)
	if err != nil {
		return nil, fmt.Errorf("hash decoy password: %w", err)
	}
	return decorator.ApplyCommandDecorators[in.LogInWithPassword](logInWithPasswordHandler{
		users: users, sessions: sessions, hasher: hasher, limiter: limiter,
		factory: factory, clock: clock, decoyHash: decoyHash,
	}, logger, metricsClient), nil
}

func (h logInWithPasswordHandler) Handle(ctx context.Context, cmd in.LogInWithPassword) error {
	sessionID, err := session.NewID(cmd.SessionID)
	if err != nil {
		return err
	}
	now := h.clock.Now()
	if wait := h.limiter.RetryAfter(cmd.Username, cmd.ClientIP, now); wait > 0 {
		return LoginThrottledError{Wait: wait}
	}

	account, err := h.findUser(ctx, cmd.Username)
	if err != nil {
		return err
	}
	if account == nil || !account.CanLogInWithPassword() {
		_, _ = h.hasher.Verify(h.decoyHash, cmd.Password)
		h.limiter.RecordFailure(cmd.Username, cmd.ClientIP, now)
		return user.ErrInvalidCredentials
	}
	matches, err := h.hasher.Verify(account.PasswordHash(), cmd.Password)
	if err != nil {
		return err
	}
	if !matches {
		h.limiter.RecordFailure(cmd.Username, cmd.ClientIP, now)
		return user.ErrInvalidCredentials
	}
	h.limiter.RecordSuccess(cmd.Username, cmd.ClientIP)

	started, err := h.factory.NewSession(sessionID, account.ID().String(), cmd.SessionToken, account.SessionGeneration(), now)
	if err != nil {
		return err
	}
	return h.sessions.AddSession(ctx, started)
}

// findUser returns nil for a username that is malformed or unknown: both are
// plain login failures.
func (h logInWithPasswordHandler) findUser(ctx context.Context, rawUsername string) (*user.User, error) {
	username, err := user.NewUsername(rawUsername)
	if err != nil {
		return nil, nil
	}
	account, err := h.users.UserByUsername(ctx, username)
	if user.IsNotFound(err) {
		return nil, nil
	}
	return account, err
}
