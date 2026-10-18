// Package errors defines slug errors: errors that clients may see, with a
// stable kebab-case slug and an ErrorType that the HTTP adapter maps to a
// status code (ddd-rules D-7, IN-3).
package errors

import "time"

// ErrorType classifies a slug error for transport mapping.
type ErrorType struct{ t string }

var (
	ErrorTypeUnknown             = ErrorType{"unknown"}
	ErrorTypeIncorrectInput      = ErrorType{"incorrect-input"}
	ErrorTypeAuthentication      = ErrorType{"authentication"}
	ErrorTypeAuthorization       = ErrorType{"authorization"}
	ErrorTypeNotFound            = ErrorType{"not-found"}
	ErrorTypeConflict            = ErrorType{"conflict"}
	ErrorTypeQuotaExceeded       = ErrorType{"quota-exceeded"}
	ErrorTypeUnsupported         = ErrorType{"unsupported"}
	ErrorTypeProviderUnavailable = ErrorType{"provider-unavailable"}
	ErrorTypeServiceUnavailable  = ErrorType{"service-unavailable"}
)

func (t ErrorType) String() string { return t.t }
func (t ErrorType) IsZero() bool   { return t == ErrorType{} }

// SlugError is the contract for errors that clients may see. Domain errors
// implement it directly, or wrap a Error built by the constructors below.
type SlugError interface {
	error
	Slug() string
	ErrorType() ErrorType
}

// RetryAfterError is implemented by slug errors that know when the same
// request may succeed again. The duration is rounded up to whole seconds and
// clamped to at least 1 by the HTTP error mapper (ADR 0020).
type RetryAfterError interface {
	SlugError
	RetryAfter() time.Duration
}

// Error is a ready-made SlugError. The message must never contain secrets
// (ddd-rules OUT-6); it is shown to clients.
type Error struct {
	message   string
	slug      string
	errorType ErrorType
}

func (e Error) Error() string        { return e.message }
func (e Error) Slug() string         { return e.slug }
func (e Error) ErrorType() ErrorType { return e.errorType }

func NewIncorrectInputError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeIncorrectInput}
}

func NewAuthenticationError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeAuthentication}
}

func NewAuthorizationError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeAuthorization}
}

func NewNotFoundError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeNotFound}
}

func NewConflictError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeConflict}
}

func NewQuotaExceededError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeQuotaExceeded}
}

func NewUnsupportedError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeUnsupported}
}

func NewProviderUnavailableError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeProviderUnavailable}
}

func NewServiceUnavailableError(message, slug string) Error {
	return Error{message: message, slug: slug, errorType: ErrorTypeServiceUnavailable}
}
