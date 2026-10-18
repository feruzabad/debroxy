// Package in holds the driving ports of the job context: commands, queries
// and the representations they return (ddd-rules A-2, R-8).
package in

import "time"

// JobView is the job representation of spec §10.2.
type JobView struct {
	ID          string
	DisplayName string
	ProviderID  string
	Kind        string
	State       string
	Telemetry   TelemetryView
	SizeBytes   int64
	ExpiresAt   *time.Time
	// Shared reveals that others hold claims, never who (NFR-04).
	Shared        bool
	SlotCharged   bool
	Delivery      DeliveryView
	Files         []FileView
	CreatedAt     time.Time
	FailureReason string
	OwnerID       string
	OwnerUsername string
	// Fingerprint lets SubmitJob compare an idempotent retry; not shown to clients.
	Fingerprint string
	// RemoteItemID and ProviderItemID are internal: delivery resolves targets
	// with them; they are never shown to clients.
	RemoteItemID   string
	ProviderItemID string
}

type TelemetryView struct {
	Progress       float64
	BytesPerSecond int64
	ETASeconds     *int64
	Peers          *int
	Stalled        bool
}

type DeliveryView struct {
	PerFile bool
	Bundle  *BundleView
}

type BundleView struct {
	Format         string
	RangeSupported bool
}

type FileView struct {
	ID        string
	Path      string
	SizeBytes int64
}

// HistoryEntry is a released job (spec §5.1 job_history).
type HistoryEntry struct {
	JobID         string
	DisplayName   string
	ProviderID    string
	Kind          string
	Name          string
	FinalState    string
	FailureReason string
	OwnerID       string
	OwnerUsername string
	Fingerprint   string
	ClaimedAt     time.Time
	ReleasedAt    time.Time
}

// QuotaUsageResult: SlotsUsed counts active items charged to the user (ADR 0006 rule 12).
type QuotaUsageResult struct {
	SlotsUsed     int
	MaxActiveJobs int
}

// DeliveryTargetResult tells delivery what to stream (spec §5.9).
type DeliveryTargetResult struct {
	ProviderID     string
	Kind           string
	ProviderItemID string
	FileID         string
	Bundle         bool
	// Name is the file's path or, for a bundle, the item name; used when the
	// link's name segment is empty (ADR 0010 rule 13).
	Name           string
	RangeSupported bool
}

// UnmanagedItemView is an item at the provider that debroxy does not track (DEC-10).
type UnmanagedItemView struct {
	ProviderID     string
	ProviderItemID string
	Kind           string
	Name           string
	SizeBytes      int64
	State          string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
}

// OutboxMessageView is a parked outbox message (ADR 0008).
type OutboxMessageView struct {
	ID           int64
	Kind         string
	RemoteItemID string
	Attempts     int
	LastError    string
	CreatedAt    time.Time
	ParkedAt     time.Time
}
