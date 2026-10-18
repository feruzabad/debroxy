package remoteitem

import (
	"errors"
	"fmt"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// Provider outcomes in job's language. The provider gateway adapter
// translates provider's neutral errors into these (DEP-5).

var (
	// ErrProviderItemNotFound: the provider does not know the item.
	ErrProviderItemNotFound = errors.New("the provider does not know this item")
	// ErrWebhookRejected: the webhook was not accepted. It reads like any
	// unknown path, so the reasons cannot be told apart (RISK-07).
	ErrWebhookRejected = commonerrors.NewNotFoundError("not found", "not-found")
	// ErrStateStale: a refresh did not run or did not complete (back-off,
	// provider failure, incomplete listing). Reads answer from stored state
	// with stale: true (C5-07); it is not a failure of the read.
	ErrStateStale = commonerrors.NewServiceUnavailableError("provider state may be stale", "provider-state-stale")
)

type ProviderNotFoundError struct{ Provider string }

func (e ProviderNotFoundError) Error() string {
	return fmt.Sprintf("provider %q not found", e.Provider)
}
func (e ProviderNotFoundError) Slug() string { return "provider-not-found" }
func (e ProviderNotFoundError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeNotFound
}

type ProviderDisabledError struct{ Provider string }

func (e ProviderDisabledError) Error() string {
	return fmt.Sprintf("provider %q is disabled", e.Provider)
}
func (e ProviderDisabledError) Slug() string { return "provider-disabled" }
func (e ProviderDisabledError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeConflict
}

// CapabilityRefusal says why a capability cannot be used (ADR 0003 rule 7).
type CapabilityRefusal struct{ s string }

var (
	CapabilityNotSupported       = CapabilityRefusal{"not-supported"}
	CapabilityNotEnabled         = CapabilityRefusal{"not-enabled"}
	CapabilityUnavailableForPlan = CapabilityRefusal{"unavailable-for-plan"}
)

type CapabilityRefusedError struct {
	Provider   string
	Capability string
	Refusal    CapabilityRefusal
}

func (e CapabilityRefusedError) Error() string {
	switch e.Refusal {
	case CapabilityNotEnabled:
		return fmt.Sprintf("%s is not enabled for provider %s", e.Capability, e.Provider)
	case CapabilityUnavailableForPlan:
		return fmt.Sprintf("the %s account's plan does not allow %s", e.Provider, e.Capability)
	}
	return fmt.Sprintf("provider %s does not support %s", e.Provider, e.Capability)
}

func (e CapabilityRefusedError) Slug() string { return "capability-" + e.Refusal.s }

func (e CapabilityRefusedError) ErrorType() commonerrors.ErrorType {
	if e.Refusal == CapabilityUnavailableForPlan {
		return commonerrors.ErrorTypeConflict
	}
	return commonerrors.ErrorTypeUnsupported
}

type UnsupportedHosterError struct{ Provider string }

func (e UnsupportedHosterError) Error() string {
	return fmt.Sprintf("provider %s does not support this hoster, or the hoster is down", e.Provider)
}
func (e UnsupportedHosterError) Slug() string { return "unsupported-hoster" }
func (e UnsupportedHosterError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type ProviderRejectedError struct {
	Provider string
	Detail   string
}

func (e ProviderRejectedError) Error() string {
	return fmt.Sprintf("provider %s rejected the request: %s", e.Provider, e.Detail)
}
func (e ProviderRejectedError) Slug() string { return "provider-rejected" }
func (e ProviderRejectedError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

type ProviderUnavailableError struct {
	Provider           string
	RetryAfterDuration time.Duration
}

func (e ProviderUnavailableError) Error() string {
	return fmt.Sprintf("provider %s is unavailable; try again later", e.Provider)
}
func (e ProviderUnavailableError) Slug() string { return "provider-unavailable" }
func (e ProviderUnavailableError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeProviderUnavailable
}
func (e ProviderUnavailableError) RetryAfter() time.Duration { return e.RetryAfterDuration }

// CreateNotSentError: the rate budget blocked the create before sending.
type CreateNotSentError struct{ RetryAfter time.Duration }

func (e CreateNotSentError) Error() string {
	return fmt.Sprintf("create not sent; retry in %s", e.RetryAfter)
}

// AmbiguousCreateError: the create may or may not exist at the provider.
type AmbiguousCreateError struct{ Cause string }

func (e AmbiguousCreateError) Error() string { return "create outcome unknown: " + e.Cause }

// IsDefiniteCreateFailure: the provider certainly created nothing (DEC-33).
func IsDefiniteCreateFailure(err error) bool {
	var notSent CreateNotSentError
	var ambiguous AmbiguousCreateError
	return err != nil && !errors.As(err, &notSent) && !errors.As(err, &ambiguous)
}
