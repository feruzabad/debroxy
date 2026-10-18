package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// DescribeRemoteItemOperation observes one item; an unknown item is ErrRemoteItemNotFound.
type DescribeRemoteItemOperation decorator.OperationHandler[DescribeRemoteItemRequest, Observation]

type DescribeRemoteItemRequest struct {
	ProviderID     string
	Kind           Kind
	ProviderItemID string
}
