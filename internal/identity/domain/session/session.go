// Package session is the Session aggregate of the identity context
// (spec §4.1): a logged-in state identified by an opaque Session Token.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// TouchInterval: lastSeenAt is updated at most once per interval (spec §4.2),
// which keeps authentication from writing on every request.
const TouchInterval = time.Minute

// minimumTokenLength: tokens are 32 random bytes, base64url (spec §4.3).
const minimumTokenLength = 43

// ErrSessionExpired covers unknown, expired and revoked sessions alike.
var ErrSessionExpired = commonerrors.NewAuthenticationError("the session is invalid or has expired; log in again", "session-expired")

// ID identifies a session (UUIDv7).
type ID struct{ s string }

func NewID(s string) (ID, error) {
	if _, err := uuid.Parse(s); err != nil {
		return ID{}, fmt.Errorf("invalid session ID %q", s)
	}
	return ID{s: strings.ToLower(s)}, nil
}

func (id ID) String() string { return id.s }
func (id ID) IsZero() bool   { return id == ID{} }

// TokenHash is the SHA-256 of a session token; the token itself is never stored.
type TokenHash struct{ s string }

// HashToken returns the stored form of a token.
func HashToken(token string) TokenHash {
	sum := sha256.Sum256([]byte(token))
	return TokenHash{s: hex.EncodeToString(sum[:])}
}

func NewTokenHashFromString(s string) (TokenHash, error) {
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) != sha256.Size {
		return TokenHash{}, errors.New("invalid token hash")
	}
	return TokenHash{s: s}, nil
}

func (h TokenHash) String() string { return h.s }
func (h TokenHash) IsZero() bool   { return h == TokenHash{} }

// FactoryConfig holds the session lifetimes (spec §11 identity.session).
type FactoryConfig struct {
	AbsoluteLifetime time.Duration
	IdleTimeout      time.Duration
}

// Factory creates and renews sessions with validated lifetimes (D-5).
type Factory struct {
	cfg FactoryConfig
}

func NewFactory(cfg FactoryConfig) (Factory, error) {
	if cfg.AbsoluteLifetime <= 0 || cfg.IdleTimeout <= 0 {
		return Factory{}, errors.New("session lifetimes must be positive")
	}
	if cfg.IdleTimeout > cfg.AbsoluteLifetime {
		return Factory{}, errors.New("session idle timeout must not exceed the absolute lifetime")
	}
	return Factory{cfg: cfg}, nil
}

func (f Factory) IsZero() bool { return f == Factory{} }

// IdleTimeout is needed to renew a session (Touch).
func (f Factory) IdleTimeout() time.Duration { return f.cfg.IdleTimeout }

// NewSession starts a session for a user at the user's current session generation.
func (f Factory) NewSession(id ID, userID string, token string, userSessionGeneration int, now time.Time) (*Session, error) {
	if f.IsZero() {
		panic("zero session factory")
	}
	switch {
	case id.IsZero():
		return nil, errors.New("empty session ID")
	case userID == "":
		return nil, errors.New("empty user ID")
	case len(token) < minimumTokenLength:
		return nil, errors.New("session token too short")
	case userSessionGeneration < 0:
		return nil, errors.New("negative session generation")
	}
	absoluteExpiresAt := now.Add(f.cfg.AbsoluteLifetime)
	s := &Session{
		id:                    id,
		userID:                userID,
		tokenHash:             HashToken(token),
		userSessionGeneration: userSessionGeneration,
		createdAt:             now,
		lastSeenAt:            now,
		absoluteExpiresAt:     absoluteExpiresAt,
		idleExpiresAt:         earliest(now.Add(f.cfg.IdleTimeout), absoluteExpiresAt),
	}
	s.record(SessionStarted{SessionID: id, UserID: userID})
	return s, nil
}

// Session is one login. userID is a plain string: aggregates never import
// each other.
type Session struct {
	id                    ID
	userID                string
	tokenHash             TokenHash
	userSessionGeneration int
	createdAt             time.Time
	lastSeenAt            time.Time
	absoluteExpiresAt     time.Time
	idleExpiresAt         time.Time
	revokedAt             time.Time
	events                []Event
}

// UnmarshalSessionFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalSessionFromDatabase(
	id ID, userID string, tokenHash TokenHash, userSessionGeneration int,
	createdAt, lastSeenAt, absoluteExpiresAt, idleExpiresAt, revokedAt time.Time,
) (*Session, error) {
	switch {
	case id.IsZero(), userID == "", tokenHash.IsZero():
		return nil, errors.New("session row lacks its ID, user ID or token hash")
	case userSessionGeneration < 0:
		return nil, errors.New("negative session generation")
	case createdAt.IsZero(), lastSeenAt.Before(createdAt), absoluteExpiresAt.Before(createdAt):
		return nil, errors.New("inconsistent session timestamps")
	}
	return &Session{
		id:                    id,
		userID:                userID,
		tokenHash:             tokenHash,
		userSessionGeneration: userSessionGeneration,
		createdAt:             createdAt,
		lastSeenAt:            lastSeenAt,
		absoluteExpiresAt:     absoluteExpiresAt,
		idleExpiresAt:         idleExpiresAt,
		revokedAt:             revokedAt,
	}, nil
}

func (s *Session) ID() ID                       { return s.id }
func (s *Session) UserID() string               { return s.userID }
func (s *Session) TokenHash() TokenHash         { return s.tokenHash }
func (s *Session) UserSessionGeneration() int   { return s.userSessionGeneration }
func (s *Session) CreatedAt() time.Time         { return s.createdAt }
func (s *Session) LastSeenAt() time.Time        { return s.lastSeenAt }
func (s *Session) AbsoluteExpiresAt() time.Time { return s.absoluteExpiresAt }
func (s *Session) IdleExpiresAt() time.Time     { return s.idleExpiresAt }

// RevokedAt is zero while the session is not revoked.
func (s *Session) RevokedAt() time.Time { return s.revokedAt }

func (s *Session) BelongsTo(userID string) bool { return s.userID == userID }

// IsActiveAt reports whether the session itself is valid at now. The user's
// status and session generation are checked by the caller.
func (s *Session) IsActiveAt(now time.Time) bool {
	return IsActive(s.revokedAt, s.absoluteExpiresAt, s.idleExpiresAt, now)
}

// Touch records activity: the idle expiry moves forward, never past the
// absolute expiry. It does nothing within TouchInterval of the last touch.
func (s *Session) Touch(now time.Time, idleTimeout time.Duration) {
	if !IsTouchDue(s.lastSeenAt, now) || !s.IsActiveAt(now) {
		return
	}
	s.lastSeenAt = now
	s.idleExpiresAt = earliest(now.Add(idleTimeout), s.absoluteExpiresAt)
}

// Revoke ends the session (log out). Revoking twice is harmless.
func (s *Session) Revoke(now time.Time) {
	if !s.revokedAt.IsZero() {
		return
	}
	s.revokedAt = now
	s.record(SessionRevoked{SessionID: s.id})
}

// FollowUserSessionGeneration keeps this session valid after its user's
// sessions were revoked by a change this session made itself (changing the
// own password revokes only the other sessions, spec §4.2).
func (s *Session) FollowUserSessionGeneration(generation int) error {
	if generation < s.userSessionGeneration {
		return errors.New("session generation cannot move backwards")
	}
	s.userSessionGeneration = generation
	return nil
}

// IsActive is the validity rule shared by the aggregate and read models:
// not revoked, and before both expiries.
func IsActive(revokedAt, absoluteExpiresAt, idleExpiresAt, now time.Time) bool {
	return revokedAt.IsZero() && now.Before(absoluteExpiresAt) && now.Before(idleExpiresAt)
}

// IsTouchDue reports whether lastSeenAt is older than TouchInterval.
func IsTouchDue(lastSeenAt, now time.Time) bool {
	return now.Sub(lastSeenAt) >= TouchInterval
}

// ExpiresAt is when a session ends if it is not touched again.
func ExpiresAt(absoluteExpiresAt, idleExpiresAt time.Time) time.Time {
	return earliest(absoluteExpiresAt, idleExpiresAt)
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Events returns the events recorded since the session was loaded (D-12).
func (s *Session) Events() []Event { return s.events }

// ClearEvents is called by repositories after persisting.
func (s *Session) ClearEvents() { s.events = nil }

func (s *Session) record(e Event) { s.events = append(s.events, e) }

// Event is a fact recorded by the Session aggregate.
type Event interface{ isSessionEvent() }

type SessionStarted struct {
	SessionID ID
	UserID    string
}

type SessionRevoked struct{ SessionID ID }

func (SessionStarted) isSessionEvent() {}
func (SessionRevoked) isSessionEvent() {}

// NotFoundError is reported by repositories for an unknown session (R-4).
type NotFoundError struct{ ID string }

func (e NotFoundError) Error() string { return fmt.Sprintf("session %q not found", e.ID) }
func (e NotFoundError) Slug() string  { return "session-expired" }
func (e NotFoundError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeAuthentication
}
