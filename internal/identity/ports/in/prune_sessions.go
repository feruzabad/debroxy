package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// PruneSessions deletes sessions that can never be used again: revoked, or
// past their absolute or idle expiry. Run periodically by the system.
type PruneSessions struct{}

type PruneSessionsHandler decorator.CommandHandler[PruneSessions]
