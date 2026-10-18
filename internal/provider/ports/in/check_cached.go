package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// CheckCachedOperation asks whether content is cached at the provider.
type CheckCachedOperation decorator.OperationHandler[CheckCachedRequest, CheckCachedResult]

type CheckCachedRequest struct {
	ProviderID     string
	Kind           Kind
	ProviderHashes []string
}

type CheckCachedResult struct {
	Items []CachedItem
}

// CachedItem: Files are listed where the provider offers them.
type CachedItem struct {
	ProviderHash string
	Cached       bool
	Files        []RemoteFile
}
