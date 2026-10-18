package user

import (
	"fmt"
	"strings"
	"unicode/utf8"
	"uuid"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// Role decides what a user may do (spec §4.1).
type Role struct{ s string }

var (
	RoleAdmin = Role{"admin"}
	RoleUser  = Role{"user"}
)

var allRoles = []Role{RoleAdmin, RoleUser}

func NewRoleFromString(s string) (Role, error) {
	for _, r := range allRoles {
		if r.s == s {
			return r, nil
		}
	}
	return Role{}, UnknownRoleError{Given: s}
}

func (r Role) String() string { return r.s }
func (r Role) IsZero() bool   { return r == Role{} }

// Status is active or disabled. A disabled user keeps their jobs but cannot
// log in, and their sessions and Signed Links stop working.
type Status struct{ s string }

var (
	StatusActive   = Status{"active"}
	StatusDisabled = Status{"disabled"}
)

var allStatuses = []Status{StatusActive, StatusDisabled}

func NewStatusFromString(s string) (Status, error) {
	for _, st := range allStatuses {
		if st.s == s {
			return st, nil
		}
	}
	return Status{}, UnknownStatusError{Given: s}
}

func (s Status) String() string { return s.s }
func (s Status) IsZero() bool   { return s == Status{} }

// ID identifies a user (UUIDv7, DEC-19).
type ID struct{ s string }

func NewID(s string) (ID, error) {
	if _, err := uuid.Parse(s); err != nil {
		return ID{}, InvalidIDError{Given: s}
	}
	return ID{s: strings.ToLower(s)}, nil
}

func (id ID) String() string { return id.s }
func (id ID) IsZero() bool   { return id == ID{} }

// Username is immutable once created (DEC-03): 3–32 characters of
// [a-z0-9._-]. Input is lowercased first, so "Alice" and "alice" are one name.
type Username struct{ s string }

const (
	minimumUsernameLength = 3
	maximumUsernameLength = 32
)

func NewUsername(s string) (Username, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if len(normalized) < minimumUsernameLength || len(normalized) > maximumUsernameLength {
		return Username{}, InvalidUsernameError{Given: s}
	}
	for _, r := range normalized {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return Username{}, InvalidUsernameError{Given: s}
		}
	}
	return Username{s: normalized}, nil
}

func (u Username) String() string { return u.s }
func (u Username) IsZero() bool   { return u == Username{} }

// Password is a new password in plain text, checked before hashing. It
// never prints its value.
type Password struct{ s string }

const (
	// MinimumPasswordLength is the shortest password identity accepts
	// (spec §4.4); exposed so cmd can publish it (spec §10.4).
	MinimumPasswordLength = 12
	// maximumPasswordBytes bounds the hashing input.
	maximumPasswordBytes = 1024
)

func NewPassword(s string) (Password, error) {
	if utf8.RuneCountInString(s) < MinimumPasswordLength {
		return Password{}, PasswordTooShortError{MinimumLength: MinimumPasswordLength}
	}
	if len(s) > maximumPasswordBytes {
		return Password{}, PasswordTooLongError{MaximumBytes: maximumPasswordBytes}
	}
	return Password{s: s}, nil
}

// Plaintext returns the password for the hasher. Call it nowhere else.
func (p Password) Plaintext() string { return p.s }
func (p Password) String() string    { return "<redacted>" }
func (p Password) GoString() string  { return "<redacted>" }

// PasswordHash is an encoded password hash (argon2id, spec §4.1). The domain
// treats it as opaque; the PasswordHasher port produces and checks it.
type PasswordHash struct{ s string }

func NewPasswordHash(s string) (PasswordHash, error) {
	if s == "" {
		return PasswordHash{}, fmt.Errorf("empty password hash")
	}
	return PasswordHash{s: s}, nil
}

func (h PasswordHash) String() string { return h.s }
func (h PasswordHash) IsZero() bool   { return h == PasswordHash{} }

// OIDCIdentity links a user to an external identity provider account (DEC-04).
type OIDCIdentity struct {
	issuer  string
	subject string
}

func NewOIDCIdentity(issuer, subject string) (OIDCIdentity, error) {
	if issuer == "" || subject == "" {
		return OIDCIdentity{}, commonerrors.NewIncorrectInputError("OIDC issuer and subject must both be set", "invalid-oidc-identity")
	}
	return OIDCIdentity{issuer: issuer, subject: subject}, nil
}

func (o OIDCIdentity) Issuer() string  { return o.issuer }
func (o OIDCIdentity) Subject() string { return o.subject }
func (o OIDCIdentity) IsZero() bool    { return o == OIDCIdentity{} }

// Actor is the user performing an action, as the identity domain sees it.
type Actor struct {
	UserID ID
	Role   Role
}
