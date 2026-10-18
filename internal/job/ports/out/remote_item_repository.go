// Package out holds the driven ports of the job context.
package out

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
)

// RemoteItemRepository stores RemoteItem aggregates. Each write method runs
// one BEGIN IMMEDIATE transaction (R-2, R-5), passes facts about other items
// through RemoteItemReads (R-10), stores only the one item (D-11), and turns
// the item's events into job_history rows and outbox messages in the same
// transaction (R-6). A unique-index violation retries the whole function.
type RemoteItemRepository interface {
	// JoinOrStart calls joinOrStartFn with the claimable item for (provider,
	// fingerprint), or nil when none exists, and stores the result.
	JoinOrStart(ctx context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint,
		joinOrStartFn func(ctx context.Context, existing *remoteitem.RemoteItem, reads RemoteItemReads) (JoinOrStartResult, error)) error
	// UpdateRemoteItem reports an unknown ID as remoteitem.NotFoundError.
	UpdateRemoteItem(ctx context.Context, id remoteitem.ID,
		updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads RemoteItemReads) (*remoteitem.RemoteItem, error)) error
	// UpdateRemoteItemByJob finds the item through one of its jobs. A job the
	// actor may not see is reported as remoteitem.JobNotFoundError (R-3).
	UpdateRemoteItemByJob(ctx context.Context, jobID remoteitem.JobID, actor remoteitem.Actor,
		updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads RemoteItemReads) (*remoteitem.RemoteItem, error)) error
	RemoteItem(ctx context.Context, id remoteitem.ID) (*remoteitem.RemoteItem, error)
	// RemoteItemsOfProvider returns every item of the provider that is not deleted.
	RemoteItemsOfProvider(ctx context.Context, provider remoteitem.ProviderID) ([]*remoteitem.RemoteItem, error)
	RemoteItemsInStates(ctx context.Context, states ...remoteitem.State) ([]*remoteitem.RemoteItem, error)
}

// JoinOrStartResult: SubmitClass ("uncached" or "cached") records an
// accepted submit in the same transaction (ADR 0013 rule 16); empty for joins.
type JoinOrStartResult struct {
	Item        *remoteitem.RemoteItem
	SubmitClass string
}

// ItemHolder is what other code may know about another item.
type ItemHolder struct {
	ID        remoteitem.ID
	State     remoteitem.State
	CreatedAt time.Time
	DeletedAt time.Time
}

// RemoteItemReads reads other aggregates inside the write transaction (R-10).
// It has no write methods.
type RemoteItemReads interface {
	// ActiveChargedItemCount counts active items charged to the user, with
	// the same predicate as RemoteItem.IsChargedTo (ADR 0006 rule 10).
	ActiveChargedItemCount(ctx context.Context, userID string) (int, error)
	SlotQuota(ctx context.Context, userID string) (slotquota.SlotQuota, bool, error)
	SubmitCountSince(ctx context.Context, userID, class string, since time.Time) (int, error)
	// OldestSubmitSince returns the earliest accepted_at of the user's submits
	// of the class within the rolling window, or a zero time when none exists.
	OldestSubmitSince(ctx context.Context, userID, class string, since time.Time) (time.Time, error)
	// ItemsHoldingProviderItem lists every item of the provider, in any state,
	// that holds the provider item ID.
	ItemsHoldingProviderItem(ctx context.Context, provider remoteitem.ProviderID, providerItemID remoteitem.ProviderItemID) ([]ItemHolder, error)
	// ClaimableItemsMatching lists claimable items with the fingerprint or the provider hash.
	ClaimableItemsMatching(ctx context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint, providerHash string) ([]ItemHolder, error)
	// IsDeletionParked: the item's DeleteRemoteItem message is parked (ADR 0008).
	IsDeletionParked(ctx context.Context, id remoteitem.ID) (bool, error)
}

// SlotQuotaRepository stores SlotQuota aggregates.
type SlotQuotaRepository interface {
	SetSlotQuota(ctx context.Context, quota slotquota.SlotQuota) error
}
