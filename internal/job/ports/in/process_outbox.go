package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ProcessOutbox executes every due outbox message, one after the other.
// The outbox processor runs it every second.
type ProcessOutbox struct{}

type ProcessOutboxHandler decorator.CommandHandler[ProcessOutbox]
