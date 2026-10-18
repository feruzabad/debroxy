package remoteitem

import (
	"errors"
	"time"
)

// Observation is one provider snapshot of the item in job's terms
// (spec §5.6). A zero State means the provider reported a state nobody
// knows; the item then keeps its state (DEC-09).
type Observation struct {
	providerItemID  ProviderItemID
	kind            Kind
	providerHash    string
	state           State
	name            string
	sizeBytes       int64
	files           []RemoteFile
	bundle          *BundleAvailability
	perFileDelivery bool
	expiresAt       *time.Time
	telemetry       Telemetry
	failureDetail   string
	observedAt      time.Time
}

// ObservationParams builds an Observation; adapters fill it from provider DTOs.
type ObservationParams struct {
	ProviderItemID  ProviderItemID
	Kind            Kind
	ProviderHash    string
	State           State // zero: unknown provider state
	Name            string
	SizeBytes       int64
	Files           []RemoteFile
	Bundle          *BundleAvailability
	PerFileDelivery bool
	ExpiresAt       *time.Time
	Telemetry       Telemetry
	FailureDetail   string
	ObservedAt      time.Time
}

func NewObservation(p ObservationParams) (Observation, error) {
	switch {
	case p.ProviderItemID.IsZero():
		return Observation{}, errors.New("observation without provider item ID")
	case p.ObservedAt.IsZero():
		return Observation{}, errors.New("observation without time")
	case !p.State.IsZero() && !isObservable(p.State):
		return Observation{}, InvalidOperationError{Operation: "observe", State: p.State}
	}
	return Observation{
		providerItemID: p.ProviderItemID, kind: p.Kind, providerHash: p.ProviderHash, state: p.State,
		name: p.Name, sizeBytes: p.SizeBytes, files: append([]RemoteFile(nil), p.Files...), bundle: p.Bundle,
		perFileDelivery: p.PerFileDelivery, expiresAt: p.ExpiresAt, telemetry: p.Telemetry,
		failureDetail: p.FailureDetail, observedAt: p.ObservedAt,
	}, nil
}

func isObservable(s State) bool {
	for _, o := range observableStates {
		if o == s {
			return true
		}
	}
	return false
}

func (o Observation) ProviderItemID() ProviderItemID { return o.providerItemID }
func (o Observation) ProviderHash() string           { return o.providerHash }
func (o Observation) State() State                   { return o.state }
func (o Observation) HasKnownState() bool            { return !o.state.IsZero() }
func (o Observation) ObservedAt() time.Time          { return o.observedAt }
func (o Observation) Kind() Kind                     { return o.kind }
func (o Observation) Name() string                   { return o.name }
func (o Observation) SizeBytes() int64               { return o.sizeBytes }

// IsActiveOrReady: the state a revive may move to (ADR 0007 rule 14).
func (o Observation) IsActiveOrReady() bool {
	return o.state.IsActive() || o.state == StateReady
}
