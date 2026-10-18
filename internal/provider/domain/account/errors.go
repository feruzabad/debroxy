package account

import (
	"fmt"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// CapabilityNotSupportedError: the provider kind lacks the capability (spec §6.3).
type CapabilityNotSupportedError struct {
	Provider   string
	Capability Capability
}

func (e CapabilityNotSupportedError) Error() string {
	return fmt.Sprintf("provider %s does not support %s", e.Provider, e.Capability)
}
func (e CapabilityNotSupportedError) Slug() string { return "capability-not-supported" }
func (e CapabilityNotSupportedError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeUnsupported
}

// CapabilityNotEnabledError: the provider has the capability, but debroxy v1
// or the operator does not enable it.
type CapabilityNotEnabledError struct {
	Provider   string
	Capability Capability
}

func (e CapabilityNotEnabledError) Error() string {
	return fmt.Sprintf("%s is not enabled for provider %s", e.Capability, e.Provider)
}
func (e CapabilityNotEnabledError) Slug() string { return "capability-not-enabled" }
func (e CapabilityNotEnabledError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeUnsupported
}

// CapabilityUnavailableForPlanError: the account's plan does not allow it.
type CapabilityUnavailableForPlanError struct {
	Provider   string
	Capability Capability
}

func (e CapabilityUnavailableForPlanError) Error() string {
	return fmt.Sprintf("the %s account's plan does not allow %s", e.Provider, e.Capability)
}
func (e CapabilityUnavailableForPlanError) Slug() string { return "capability-unavailable-for-plan" }
func (e CapabilityUnavailableForPlanError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeConflict
}

// NotFoundError: no Provider Account with this ID (R-4).
type NotFoundError struct{ ID string }

func (e NotFoundError) Error() string                     { return fmt.Sprintf("provider %q not found", e.ID) }
func (e NotFoundError) Slug() string                      { return "provider-not-found" }
func (e NotFoundError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeNotFound }

// DisabledError: the Provider Account exists but is disabled in configuration.
type DisabledError struct{ ID string }

func (e DisabledError) Error() string                     { return fmt.Sprintf("provider %q is disabled", e.ID) }
func (e DisabledError) Slug() string                      { return "provider-disabled" }
func (e DisabledError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeConflict }
