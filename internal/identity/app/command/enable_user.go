package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type enableUserHandler struct {
	users out.UserRepository
}

func NewEnableUserHandler(users out.UserRepository, logger *slog.Logger, metricsClient decorator.MetricsClient) in.EnableUserHandler {
	if users == nil {
		panic("nil users")
	}
	return decorator.ApplyCommandDecorators[in.EnableUser](enableUserHandler{users: users}, logger, metricsClient)
}

func (h enableUserHandler) Handle(ctx context.Context, cmd in.EnableUser) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	return h.users.UpdateUser(ctx, id, func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
		u.Enable()
		return u, nil
	})
}
