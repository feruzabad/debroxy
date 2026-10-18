package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type linkOIDCIdentityHandler struct {
	users out.UserRepository
}

func NewLinkOIDCIdentityHandler(users out.UserRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.LinkOIDCIdentityHandler {
	if users == nil {
		panic("nil users")
	}
	return decorator.ApplyCommandDecorators[in.LinkOIDCIdentity](linkOIDCIdentityHandler{users: users}, logger, metricsClient)
}

func (h linkOIDCIdentityHandler) Handle(ctx context.Context, cmd in.LinkOIDCIdentity) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	identity, err := user.NewOIDCIdentity(cmd.Issuer, cmd.Subject)
	if err != nil {
		return err
	}
	return h.users.UpdateUser(ctx, id, func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error) {
		if err := ensureOIDCIdentityFree(ctx, reads, identity, u.ID()); err != nil {
			return nil, err
		}
		return u, u.LinkOIDCIdentity(identity)
	})
}

// ensureOIDCIdentityFree reads, in the write transaction, whether another
// user holds the identity (R-10); the unique index is the backstop.
func ensureOIDCIdentityFree(ctx context.Context, reads out.UserPolicyReads, identity user.OIDCIdentity, self user.ID) error {
	holder, found, err := reads.OIDCIdentityHolder(ctx, identity)
	if err != nil {
		return err
	}
	if found && holder != self.String() {
		return user.OIDCIdentityTakenError{}
	}
	return nil
}
