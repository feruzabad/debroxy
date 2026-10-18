package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ProbeProviderCapabilities lets the next use test every unavailable
// capability of the provider again (admin only, ADR 0003 rule 8, C9-03).
type ProbeProviderCapabilities struct {
	Actor      Actor
	ProviderID string
}

type ProbeProviderCapabilitiesHandler decorator.CommandHandler[ProbeProviderCapabilities]
