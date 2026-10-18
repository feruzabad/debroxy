// Package authenticator implements common/server.Authenticator on top of
// the identity AuthenticatedActor query (DEC-22). identity/service builds
// it; cmd passes it to the auth middleware.
package authenticator

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/auth"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

type Authenticator struct {
	app    in.Application
	logger *slog.Logger
}

func NewAuthenticator(app in.Application, logger *slog.Logger) Authenticator {
	if app.Queries.AuthenticatedActor == nil || app.Commands.TouchSession == nil {
		panic("nil identity application")
	}
	if logger == nil {
		panic("nil logger")
	}
	return Authenticator{app: app, logger: logger}
}

// Check resolves the session token again without touching the session, so
// a long-lived request does not extend the idle timeout (C14-07).
func (a Authenticator) Check(ctx context.Context, credential string) error {
	_, err := a.app.Queries.AuthenticatedActor.Handle(ctx, in.AuthenticatedActor{SessionToken: credential})
	return err
}

// Authenticate resolves a session token. When the session is due for a
// touch (at most once per minute) it sends TouchSession; a failed touch is
// logged and does not fail the request.
func (a Authenticator) Authenticate(ctx context.Context, credential string) (auth.Principal, error) {
	result, err := a.app.Queries.AuthenticatedActor.Handle(ctx, in.AuthenticatedActor{SessionToken: credential})
	if err != nil {
		return auth.Principal{}, err
	}
	if result.TouchDue {
		if err := a.app.Commands.TouchSession.Handle(ctx, in.TouchSession{SessionID: result.SessionID}); err != nil {
			a.logger.WarnContext(ctx, "touch session failed", "error", err.Error())
		}
	}
	return auth.Principal{
		UserID: result.UserID, Role: result.Role, SessionID: result.SessionID,
		SessionIdleExpiresAt: result.SessionIdleExpiresAt, SessionAbsoluteExpiresAt: result.SessionAbsoluteExpiresAt,
	}, nil
}
