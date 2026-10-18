package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ListProvidersOperation lists the configured providers with their capabilities (spec §6.2).
type ListProvidersOperation decorator.OperationHandler[ListProvidersRequest, []ProviderSummary]

type ListProvidersRequest struct{}

type ProviderSummary struct {
	ID           string
	Kind         string
	DisplayName  string
	Enabled      bool
	Capabilities []CapabilityView
}

// CapabilityView is one capability of one account: whether the provider kind
// has it, whether debroxy enables it, and the learned state (spec §6.3).
type CapabilityView struct {
	Name      string
	Supported bool
	Enabled   bool
	// State is available, unavailable or unverified; empty when not supported.
	State string
}
