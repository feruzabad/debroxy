package remoteitem

import (
	"fmt"
	"slices"
	"time"
)

// DatabaseFields is every stored field of a RemoteItem.
type DatabaseFields struct {
	ID                   ID
	ProviderID           ProviderID
	ProviderItemID       ProviderItemID
	ProviderHash         string
	Kind                 Kind
	Fingerprint          Fingerprint
	Source               Source
	Name                 string
	SizeBytes            int64
	State                State
	FailureReason        FailureReason
	Files                []RemoteFile
	Bundle               *BundleAvailability
	PerFileDelivery      bool
	ExpiresAt            *time.Time
	Telemetry            Telemetry
	LastProgressAt       time.Time
	LastObservationAt    time.Time
	Submission           *Submission
	CreatedAt            time.Time
	DeletedAt            time.Time
	Claims               []Job
	ChargeHolder         JobID
	ChargeHolderReleased bool
}

// UnmarshalRemoteItemFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalRemoteItemFromDatabase(f DatabaseFields) (*RemoteItem, error) {
	lifecycle, err := NewLifecycle(f.State)
	if err != nil {
		return nil, err
	}
	r := &RemoteItem{
		id: f.ID, providerID: f.ProviderID, providerItemID: f.ProviderItemID, providerHash: f.ProviderHash,
		kind: f.Kind, fingerprint: f.Fingerprint, source: f.Source, name: f.Name, sizeBytes: f.SizeBytes,
		lifecycle: lifecycle, failureReason: f.FailureReason, files: slices.Clone(f.Files), bundle: f.Bundle,
		perFileDelivery: f.PerFileDelivery, expiresAt: f.ExpiresAt, telemetry: f.Telemetry,
		lastProgressAt: f.LastProgressAt, lastObservationAt: f.LastObservationAt, submission: f.Submission,
		createdAt: f.CreatedAt, deletedAt: f.DeletedAt, claims: slices.Clone(f.Claims),
		chargeHolder: f.ChargeHolder, chargeReleased: f.ChargeHolderReleased,
	}
	if err := r.checkInvariants(); err != nil {
		return nil, fmt.Errorf("stored remote item %s: %w", f.ID, err)
	}
	return r, nil
}

// checkInvariants: ADR 0004 I-1…I-3, ADR 0006 CH-1…CH-3 and the
// submission invariants of ADR 0005.
func (r *RemoteItem) checkInvariants() error {
	state := r.State()
	owners, ids := map[string]bool{}, map[JobID]bool{}
	for _, job := range r.claims {
		if owners[job.ownerID] || ids[job.id] {
			return fmt.Errorf("I-1: two claims share an owner or job ID")
		}
		owners[job.ownerID], ids[job.id] = true, true
	}
	switch {
	case state.IsClaimable() && len(r.claims) == 0:
		return fmt.Errorf("I-2: claimable item without claims")
	case (state == StateDeleting || state == StateDeleted) && len(r.claims) > 0:
		return fmt.Errorf("I-3: %s item with claims", state)
	case !r.chargeHolder.IsZero() && (!ids[r.chargeHolder] || r.chargeReleased):
		return fmt.Errorf("CH-1: charge holder is not a claim, or the charge was spread")
	case !state.IsActive() && (!r.chargeHolder.IsZero() || r.chargeReleased):
		return fmt.Errorf("CH-2: inactive item with charge fields")
	case state.IsActive() && r.chargeHolder.IsZero() == !r.chargeReleased:
		return fmt.Errorf("CH-3: active item needs exactly one of charge holder or spread charge")
	}
	needsSubmission := state == StateSubmitting || (state == StateDeleting && r.providerItemID.IsZero())
	switch {
	case needsSubmission != (r.submission != nil):
		return fmt.Errorf("submission present iff submitting or deleting without provider item")
	case needsSubmission != !r.source.IsZero():
		return fmt.Errorf("source present iff submitting or deleting without provider item")
	case state == StateSubmitting && !r.providerItemID.IsZero():
		return fmt.Errorf("provider item ID while submitting")
	case (state == StateDeleted) != !r.deletedAt.IsZero():
		return fmt.Errorf("deletedAt set iff deleted")
	}
	return nil
}
