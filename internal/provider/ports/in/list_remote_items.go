package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ListRemoteItemsOperation lists every item of the usable kinds (spec §5.6 step 1).
type ListRemoteItemsOperation decorator.OperationHandler[ListRemoteItemsRequest, RemoteItemListing]

type ListRemoteItemsRequest struct {
	ProviderID string
}
