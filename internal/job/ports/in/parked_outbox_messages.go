package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ParkedOutboxMessages lists outbox messages that stopped retrying (admin only).
type ParkedOutboxMessages struct {
	Actor Actor
}

type ParkedOutboxMessagesHandler decorator.QueryHandler[ParkedOutboxMessages, []OutboxMessageView]
