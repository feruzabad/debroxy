package out

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

// MessageKindDeleteRemoteItem is the only outbox message (spec §5.7).
const MessageKindDeleteRemoteItem = "DeleteRemoteItem"

// OutboxMessage is a recorded side effect, executed at least once (ADR 0008).
type OutboxMessage struct {
	ID           int64
	Kind         string
	RemoteItemID remoteitem.ID
	Attempts     int
	CreatedAt    time.Time
}

// OutboxRepository serves the outbox processor. Messages are written by
// RemoteItemRepository in the item's transaction.
type OutboxRepository interface {
	DueMessages(ctx context.Context, now time.Time, limit int) ([]OutboxMessage, error)
	MarkProcessed(ctx context.Context, id int64, now time.Time) error
	// RecordFailure counts a failed attempt; park stops further attempts.
	RecordFailure(ctx context.Context, id int64, nextAttemptAt time.Time, lastError string, park bool, now time.Time) error
}

// SyncStateRepository remembers the last complete listing per provider (spec §5.6).
type SyncStateRepository interface {
	RecordAuthoritativeSync(ctx context.Context, provider remoteitem.ProviderID, at time.Time) error
	LastAuthoritativeSync(ctx context.Context, provider remoteitem.ProviderID) (time.Time, error)
}

// UnmanagedItem is a provider item no tracked item or submission matches (DEC-10).
type UnmanagedItem struct {
	ProviderItemID string
	Kind           string
	Name           string
	SizeBytes      int64
	State          string
}

// UnmanagedItemRepository records the leftovers of each listing.
type UnmanagedItemRepository interface {
	RecordUnmanagedItems(ctx context.Context, provider remoteitem.ProviderID, items []UnmanagedItem, seenAt time.Time) error
}

// RetentionCutoffs: records older than each cutoff are pruned (spec §5.8, DEC-20).
type RetentionCutoffs struct {
	JobHistoryReleasedBefore time.Time
	UnmanagedItemsSeenBefore time.Time
	ProcessedOutboxBefore    time.Time
	DeletedItemsBefore       time.Time
	SubmitRateWindowsBefore  time.Time
}

// RetentionRepository prunes records nobody needs any more.
type RetentionRepository interface {
	PruneExpiredRecords(ctx context.Context, cutoffs RetentionCutoffs) error
}
