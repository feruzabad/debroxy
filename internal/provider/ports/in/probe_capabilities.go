package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ProbeCapabilitiesOperation lets one call test every unavailable capability again (admin, ADR 0003 rule 8).
type ProbeCapabilitiesOperation decorator.OperationHandler[ProbeCapabilitiesRequest, []CapabilityView]

type ProbeCapabilitiesRequest struct {
	ProviderID string
}
