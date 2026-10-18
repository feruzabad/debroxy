package in

import "github.com/feruzabad/debroxy/internal/job/domain/remoteitem"

// NewJobView builds the representation of one job on its item. Read models
// of every storage use it, so the mapping exists once.
func NewJobView(item *remoteitem.RemoteItem, job remoteitem.Job) JobView {
	telemetry := item.Telemetry()
	view := JobView{
		ID:             job.ID().String(),
		DisplayName:    job.DisplayName(),
		ProviderID:     item.ProviderID().String(),
		Kind:           item.Kind().String(),
		State:          item.State().String(),
		SizeBytes:      item.SizeBytes(),
		ExpiresAt:      item.ExpiresAt(),
		Shared:         len(item.Claims()) > 1,
		SlotCharged:    item.IsChargedTo(job.OwnerID()),
		Delivery:       DeliveryView{PerFile: item.PerFileDelivery()},
		CreatedAt:      job.ClaimedAt(),
		FailureReason:  item.FailureReason().Code(),
		OwnerID:        job.OwnerID(),
		OwnerUsername:  job.OwnerUsername(),
		Fingerprint:    item.Fingerprint().String(),
		RemoteItemID:   item.ID().String(),
		ProviderItemID: item.ProviderItemID().String(),
		Telemetry: TelemetryView{
			Progress: telemetry.Progress, BytesPerSecond: telemetry.BytesPerSecond,
			Peers: telemetry.Peers, Stalled: telemetry.Stalled,
		},
	}
	if view.DisplayName == "" {
		view.DisplayName = item.Name()
	}
	if telemetry.ETA != nil {
		seconds := int64(telemetry.ETA.Seconds())
		view.Telemetry.ETASeconds = &seconds
	}
	if bundle := item.Bundle(); bundle != nil {
		view.Delivery.Bundle = &BundleView{Format: bundle.Format().String(), RangeSupported: bundle.RangeSupported()}
	}
	for _, f := range item.Files() {
		view.Files = append(view.Files, FileView{ID: f.FileID(), Path: f.Path(), SizeBytes: f.SizeBytes()})
	}
	return view
}
