package user

import (
	"errors"
	"fmt"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

var (
	// ErrLastAdmin protects the last active admin from being disabled or demoted.
	ErrLastAdmin = commonerrors.NewConflictError("the last active admin cannot be disabled or demoted", "last-admin")
	// ErrNoLoginMethod: a user needs a password or an OIDC identity.
	ErrNoLoginMethod = commonerrors.NewIncorrectInputError("a user needs a password or an OIDC identity", "no-login-method")
	// ErrForbidden: the actor's role does not allow the action.
	ErrForbidden = commonerrors.NewAuthorizationError("this action needs the admin role", "forbidden")
	// ErrBootstrapNotAllowed: BootstrapAdmin runs only while no user exists.
	ErrBootstrapNotAllowed = commonerrors.NewConflictError("users already exist; the first admin can only be created on an empty database", "bootstrap-not-allowed")
	// ErrInvalidCredentials is the uniform login failure (spec §4.2): unknown
	// user, wrong password and disabled user look the same.
	ErrInvalidCredentials = commonerrors.NewAuthenticationError("invalid username or password", "invalid-credentials")
)

type UnknownRoleError struct{ Given string }

func (e UnknownRoleError) Error() string {
	return fmt.Sprintf("unknown role %q; allowed: %s, %s", e.Given, RoleAdmin, RoleUser)
}
func (e UnknownRoleError) Slug() string { return "invalid-role" }
func (e UnknownRoleError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type UnknownStatusError struct{ Given string }

func (e UnknownStatusError) Error() string {
	return fmt.Sprintf("unknown status %q; allowed: %s, %s", e.Given, StatusActive, StatusDisabled)
}
func (e UnknownStatusError) Slug() string { return "invalid-status" }
func (e UnknownStatusError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type InvalidIDError struct{ Given string }

func (e InvalidIDError) Error() string                     { return fmt.Sprintf("invalid user ID %q", e.Given) }
func (e InvalidIDError) Slug() string                      { return "user-not-found" }
func (e InvalidIDError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeNotFound }

type InvalidUsernameError struct{ Given string }

func (e InvalidUsernameError) Error() string {
	return fmt.Sprintf("invalid username %q: use %d–%d characters of a-z, 0-9, '.', '_' and '-'",
		e.Given, minimumUsernameLength, maximumUsernameLength)
}
func (e InvalidUsernameError) Slug() string { return "invalid-username" }
func (e InvalidUsernameError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type PasswordTooShortError struct{ MinimumLength int }

func (e PasswordTooShortError) Error() string {
	return fmt.Sprintf("password must have at least %d characters", e.MinimumLength)
}
func (e PasswordTooShortError) Slug() string { return "invalid-password" }
func (e PasswordTooShortError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type PasswordTooLongError struct{ MaximumBytes int }

func (e PasswordTooLongError) Error() string {
	return fmt.Sprintf("password must have at most %d bytes", e.MaximumBytes)
}
func (e PasswordTooLongError) Slug() string { return "invalid-password" }
func (e PasswordTooLongError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

// UsernameTakenError is reported by repositories when the unique username is in use.
type UsernameTakenError struct{ Username string }

func (e UsernameTakenError) Error() string                     { return fmt.Sprintf("username %q is taken", e.Username) }
func (e UsernameTakenError) Slug() string                      { return "username-taken" }
func (e UsernameTakenError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeConflict }

// NotFoundError is reported by repositories for an unknown user (R-4).
type NotFoundError struct{ ID string }

func (e NotFoundError) Error() string                     { return fmt.Sprintf("user %q not found", e.ID) }
func (e NotFoundError) Slug() string                      { return "user-not-found" }
func (e NotFoundError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeNotFound }

// IsNotFound reports whether err means an unknown user.
func IsNotFound(err error) bool {
	var notFound NotFoundError
	return errors.As(err, &notFound)
}

// OIDCIdentityTakenError: another user is linked to this OIDC identity.
type OIDCIdentityTakenError struct{}

func (e OIDCIdentityTakenError) Error() string { return "this OIDC identity is linked to another user" }
func (e OIDCIdentityTakenError) Slug() string  { return "oidc-identity-taken" }
func (e OIDCIdentityTakenError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeConflict
}
