package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type disableUserHandler struct {
	users out.UserRepository
}

func NewDisableUserHandler(users out.UserRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.DisableUserHandler {
	if users == nil {
		panic("nil users")
	}
	return decorator.ApplyCommandDecorators[in.DisableUser](disableUserHandler{users: users}, logger, metricsClient)
}

func (h disableUserHandler) Handle(ctx context.Context, cmd in.DisableUser) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	return h.users.UpdateUser(ctx, id, func(ctx context.Context, u *user.User, reads out.UserPolicyReads) (*user.User, error) {
		activeAdmins, err := reads.ActiveAdminCount(ctx)
		if err != nil {
			return nil, err
		}
		if err := u.Disable(activeAdmins); err != nil {
			return nil, err
		}
		return u, nil
	})
}
