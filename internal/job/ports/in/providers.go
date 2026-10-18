package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// Providers lists the configured providers with their capabilities (C9-03).
type Providers struct {
	Actor Actor
}

// ProviderView is one Provider Account as users see it.
type ProviderView struct {
	ID           string
	Kind         string
	DisplayName  string
	Enabled      bool
	Capabilities []CapabilityView
}

// CapabilityView: whether the provider kind has the capability, whether
// debroxy enables it, and the learned state (available, unavailable,
// unverified; empty when not supported).
type CapabilityView struct {
	Name      string
	Supported bool
	Enabled   bool
	State     string
}

type ProvidersHandler decorator.QueryHandler[Providers, []ProviderView]
