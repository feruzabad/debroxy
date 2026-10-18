package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// CreateRemoteItemOperation creates an item at the provider. Errors: the capability errors of ADR 0003 rule 7, CreateNotSentError, AmbiguousCreateError, ProviderRejectedError, UnsupportedHosterError, ProviderUnavailableError (before sending only).
type CreateRemoteItemOperation decorator.OperationHandler[CreateRemoteItemRequest, CreateRemoteItemResult]

type CreateRemoteItemRequest struct {
	ProviderID string
	Source     Source
	// Cached: CheckCached reported this content cached in the same
	// submission, so the create uses the cheaper rate class (ADR 0013 rule 4).
	Cached bool
}

type CreateRemoteItemResult struct {
	ProviderItemID string
	ProviderHash   string
	Observation    Observation
}
