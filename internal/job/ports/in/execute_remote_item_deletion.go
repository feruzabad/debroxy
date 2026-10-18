package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ExecuteRemoteItemDeletion runs one DeleteRemoteItem outbox message (spec
// §5.7): supersede or delete at the provider, then confirm. A failure is
// recorded on the message for a later retry; after the last attempt the
// message is parked (ADR 0008).
type ExecuteRemoteItemDeletion struct {
	MessageID    int64
	RemoteItemID string
	// Attempts is the number of failed attempts so far.
	Attempts int
}

type ExecuteRemoteItemDeletionHandler decorator.CommandHandler[ExecuteRemoteItemDeletion]
