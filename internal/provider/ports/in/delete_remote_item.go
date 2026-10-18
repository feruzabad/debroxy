package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// DeleteRemoteItemOperation deletes an item: nil error means deleted, confirmed directly or by look-up; any error means retry (ADR 0003 rule 5).
type DeleteRemoteItemOperation decorator.OperationHandler[DeleteRemoteItemRequest, DeleteRemoteItemResult]

type DeleteRemoteItemRequest struct {
	ProviderID     string
	Kind           Kind
	ProviderItemID string
}

type DeleteRemoteItemResult struct{}
