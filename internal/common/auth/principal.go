// Package auth carries the authenticated principal through a request
// context. The common/server auth middleware sets it (DEC-22); each
// context's HTTP adapter converts it to its own ports/in.Actor (DEP-6).
package auth

import (
	"context"
	"time"
)

// Principal is who made a request: a user ID, a role name and the session
// the request came with. It holds no rules; what a role may do is decided
// inside each context.
type Principal struct {
	UserID    string
	Role      string
	SessionID string
	// SessionIdleExpiresAt is when the session ends unless it is used again,
	// already including this request's touch (spec §10.4). Zero when the
	// request is not session-authenticated.
	SessionIdleExpiresAt time.Time
	// SessionAbsoluteExpiresAt is when the session ends at the latest, zero
	// when the request is not session-authenticated.
	SessionAbsoluteExpiresAt time.Time
}

type principalContextKey struct{}

// WithPrincipal returns a copy of ctx that carries principal.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the principal of an authenticated request.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// CredentialCheck re-checks the request's credential without touching the
// session: it fails once the session has ended or the user is no longer
// active. Long-lived requests such as Event Streams call it periodically
// (spec §5.11, C14-07).
type CredentialCheck func(ctx context.Context) error

type credentialCheckContextKey struct{}

func WithCredentialCheck(ctx context.Context, check CredentialCheck) context.Context {
	return context.WithValue(ctx, credentialCheckContextKey{}, check)
}

func CredentialCheckFromContext(ctx context.Context) (CredentialCheck, bool) {
	check, ok := ctx.Value(credentialCheckContextKey{}).(CredentialCheck)
	return check, ok
}
