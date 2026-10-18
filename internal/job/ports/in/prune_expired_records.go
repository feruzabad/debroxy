package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// PruneExpiredRecords deletes records past their retention: job history,
// unmanaged items no longer seen, processed outbox messages, deleted items
// and old submit-rate rows (spec §5.8).
type PruneExpiredRecords struct{}

type PruneExpiredRecordsHandler decorator.CommandHandler[PruneExpiredRecords]
