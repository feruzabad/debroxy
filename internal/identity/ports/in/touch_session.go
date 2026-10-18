package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// TouchSession records activity on a session, at most once per minute. The
// authenticator sends it when AuthenticatedActor reports TouchDue.
type TouchSession struct {
	SessionID string
}

type TouchSessionHandler decorator.CommandHandler[TouchSession]
