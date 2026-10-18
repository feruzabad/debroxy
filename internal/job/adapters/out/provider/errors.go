package provider

import (
	"errors"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

var refusals = map[providerin.CapabilityRefusal]remoteitem.CapabilityRefusal{
	providerin.CapabilityNotSupported:       remoteitem.CapabilityNotSupported,
	providerin.CapabilityNotEnabled:         remoteitem.CapabilityNotEnabled,
	providerin.CapabilityUnavailableForPlan: remoteitem.CapabilityUnavailableForPlan,
}

// translateError maps provider's neutral errors to job's errors (S4-F4).
// Unknown errors pass through unchanged.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var (
		capability  providerin.CapabilityRefusedError
		notFound    providerin.ProviderNotFoundError
		disabled    providerin.ProviderDisabledError
		unavailable providerin.ProviderUnavailableError
		rejected    providerin.ProviderRejectedError
		hoster      providerin.UnsupportedHosterError
		notSent     providerin.CreateNotSentError
		ambiguous   providerin.AmbiguousCreateError
	)
	switch {
	case errors.As(err, &capability):
		return remoteitem.CapabilityRefusedError{Provider: capability.Provider, Capability: capability.Capability, Refusal: refusals[capability.Refusal]}
	case errors.As(err, &notFound):
		return remoteitem.ProviderNotFoundError{Provider: notFound.Provider}
	case errors.As(err, &disabled):
		return remoteitem.ProviderDisabledError{Provider: disabled.Provider}
	case errors.As(err, &unavailable):
		return remoteitem.ProviderUnavailableError{Provider: unavailable.Provider, RetryAfterDuration: unavailable.RetryAfter}
	case errors.As(err, &rejected):
		return remoteitem.ProviderRejectedError{Provider: rejected.Provider, Detail: rejected.Detail}
	case errors.As(err, &hoster):
		return remoteitem.UnsupportedHosterError{Provider: hoster.Provider}
	case errors.As(err, &notSent):
		return remoteitem.CreateNotSentError{RetryAfter: notSent.RetryAfter}
	case errors.As(err, &ambiguous):
		return remoteitem.AmbiguousCreateError{Cause: ambiguous.Cause}
	case errors.Is(err, providerin.ErrRemoteItemNotFound):
		return remoteitem.ErrProviderItemNotFound
	case errors.Is(err, providerin.ErrWebhookRejected):
		return remoteitem.ErrWebhookRejected
	}
	return err
}
