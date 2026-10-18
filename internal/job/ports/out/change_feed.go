package out

import "github.com/feruzabad/debroxy/internal/job/domain/remoteitem"

// ChangeNotifier receives committed changes for the Event Streams (ADR 0021
// rule 5). The RemoteItem repository calls it after a transaction commits and
// never for a rolled-back one; the Refresher calls it when a provider's
// back-off starts or ends. Implementations never block the caller (A-9).
type ChangeNotifier interface {
	ItemChanged(change ItemChange)
	ProviderStaleChanged(change ProviderStaleChange)
}

// ItemChange names a committed RemoteItem change by IDs only.
type ItemChange struct {
	RemoteItemID remoteitem.ID
	ProviderID   remoteitem.ProviderID
	// OwnersBefore and OwnersAfter are the user IDs owning the item's claims
	// before and after the change.
	OwnersBefore []string
	OwnersAfter  []string
	ReleasedJobs []ReleasedJob
}

// ReleasedJob is a job the change released, with its owner for scope filtering.
type ReleasedJob struct {
	JobID   remoteitem.JobID
	OwnerID string
}

// ProviderStaleChange reports that a provider's back-off started (Stale) or ended.
type ProviderStaleChange struct {
	ProviderID remoteitem.ProviderID
	Stale      bool
}

// ChangeNotice is one change handed to a subscriber; exactly one field is set.
type ChangeNotice struct {
	Item          *ItemChange
	ProviderStale *ProviderStaleChange
}

// ChangeSubscriptions hands change notices to subscribers (ADR 0021 rule 6).
type ChangeSubscriptions interface {
	Subscribe() ChangeSubscription
}

// ChangeSubscription receives notices on a bounded queue. When the queue is
// full a notice is dropped and the subscription is overrun: Overrun is closed
// and no further notices arrive. Close ends the subscription; it is idempotent.
type ChangeSubscription interface {
	Notices() <-chan ChangeNotice
	Overrun() <-chan struct{}
	Close()
}
