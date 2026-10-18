package in

import (
	"errors"
	"fmt"
	"time"
)

// The neutral results of spec §6.4. Messages never contain secrets.
// Consumers translate them into their own errors at their adapter (DEP-5);
// they carry no slugs.

// ErrRemoteItemNotFound: the provider does not know the item.
var ErrRemoteItemNotFound = errors.New("the provider does not know this item")

// ErrWebhookRejected: unknown provider, wrong secret or webhooks not
// accepted. The cases are not told apart (RISK-07).
var ErrWebhookRejected = errors.New("webhook rejected")

// ProviderUnavailableError: network failure, 5xx, open circuit, or an
// exhausted rate budget for a call other than a create.
type ProviderUnavailableError struct {
	Provider   string
	RetryAfter time.Duration
	Cause      string
}

func (e ProviderUnavailableError) Error() string {
	return fmt.Sprintf("provider %s is unavailable: %s", e.Provider, e.Cause)
}

// ProviderRejectedError: the provider refused the request (a definite failure).
type ProviderRejectedError struct {
	Provider string
	Detail   string
}

func (e ProviderRejectedError) Error() string {
	return fmt.Sprintf("provider %s rejected the request: %s", e.Provider, e.Detail)
}

// UnsupportedHosterError: the URL is not on the provider's hoster catalogue.
type UnsupportedHosterError struct {
	Provider string
}

func (e UnsupportedHosterError) Error() string {
	return fmt.Sprintf("provider %s does not support this hoster", e.Provider)
}

// CreateNotSentError: the rate budget blocked a create before it was sent
// (ADR 0013 rule 8). Nothing reached the provider.
type CreateNotSentError struct {
	Provider   string
	RetryAfter time.Duration
}

func (e CreateNotSentError) Error() string {
	return fmt.Sprintf("create for provider %s not sent: rate budget exhausted, retry in %s", e.Provider, e.RetryAfter.Round(time.Second))
}

// AmbiguousCreateError: the create may or may not have reached the provider
// (timeout, network error after sending, 429, unknown 5xx). Follow-up
// refreshes resolve it (spec §5.5, C6-03).
type AmbiguousCreateError struct {
	Provider string
	Cause    string
}

func (e AmbiguousCreateError) Error() string {
	return fmt.Sprintf("create for provider %s has an unknown outcome: %s", e.Provider, e.Cause)
}

// CapabilityRefusal says why a capability check failed (ADR 0003 rule 7).
type CapabilityRefusal string

const (
	CapabilityNotSupported       CapabilityRefusal = "not-supported"
	CapabilityNotEnabled         CapabilityRefusal = "not-enabled"
	CapabilityUnavailableForPlan CapabilityRefusal = "unavailable-for-plan"
)

// CapabilityRefusedError: the call needs a capability the account cannot
// use; the provider was not called.
type CapabilityRefusedError struct {
	Provider   string
	Capability string
	Refusal    CapabilityRefusal
}

func (e CapabilityRefusedError) Error() string {
	return fmt.Sprintf("provider %s: capability %s is %s", e.Provider, e.Capability, e.Refusal)
}

// ProviderNotFoundError: no Provider Account with this ID.
type ProviderNotFoundError struct{ Provider string }

func (e ProviderNotFoundError) Error() string {
	return fmt.Sprintf("provider %q not found", e.Provider)
}

// ProviderDisabledError: the account exists but is disabled in configuration.
type ProviderDisabledError struct{ Provider string }

func (e ProviderDisabledError) Error() string {
	return fmt.Sprintf("provider %q is disabled", e.Provider)
}
