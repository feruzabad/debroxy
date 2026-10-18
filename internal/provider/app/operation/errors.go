package operation

import (
	"context"
	"errors"

	"github.com/feruzabad/debroxy/internal/common/decorator"

	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// neutralError turns domain errors into the ports/in errors consumers can
// recognise: they may import only provider's ports/in (DEP-4, DEP-5).
func neutralError(err error) error {
	var notSupported account.CapabilityNotSupportedError
	var notEnabled account.CapabilityNotEnabledError
	var unavailable account.CapabilityUnavailableForPlanError
	var notFound account.NotFoundError
	var disabled account.DisabledError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &notSupported):
		return in.CapabilityRefusedError{Provider: notSupported.Provider, Capability: notSupported.Capability.String(), Refusal: in.CapabilityNotSupported}
	case errors.As(err, &notEnabled):
		return in.CapabilityRefusedError{Provider: notEnabled.Provider, Capability: notEnabled.Capability.String(), Refusal: in.CapabilityNotEnabled}
	case errors.As(err, &unavailable):
		return in.CapabilityRefusedError{Provider: unavailable.Provider, Capability: unavailable.Capability.String(), Refusal: in.CapabilityUnavailableForPlan}
	case errors.As(err, &notFound):
		return in.ProviderNotFoundError{Provider: notFound.ID}
	case errors.As(err, &disabled):
		return in.ProviderDisabledError{Provider: disabled.ID}
	}
	return err
}

// withNeutralErrors wraps an operation so every error it returns is neutral.
func withNeutralErrors[Req any, Res any](base decorator.OperationHandler[Req, Res]) decorator.OperationHandler[Req, Res] {
	return neutralErrorsOperation[Req, Res]{base: base}
}

type neutralErrorsOperation[Req any, Res any] struct {
	base decorator.OperationHandler[Req, Res]
}

func (o neutralErrorsOperation[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	result, err := o.base.Handle(ctx, request)
	return result, neutralError(err)
}
