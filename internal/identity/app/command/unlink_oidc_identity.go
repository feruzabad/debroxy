package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type unlinkOIDCIdentityHandler struct {
	users out.UserRepository
}

func NewUnlinkOIDCIdentityHandler(users out.UserRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.UnlinkOIDCIdentityHandler {
	if users == nil {
		panic("nil users")
	}
	return decorator.ApplyCommandDecorators[in.UnlinkOIDCIdentity](unlinkOIDCIdentityHandler{users: users}, logger, metricsClient)
}

func (h unlinkOIDCIdentityHandler) Handle(ctx context.Context, cmd in.UnlinkOIDCIdentity) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	return h.users.UpdateUser(ctx, id, func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
		return u, u.UnlinkOIDCIdentity()
	})
}
