package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type resetUserPasswordHandler struct {
	users  out.UserRepository
	hasher out.PasswordHasher
}

func NewResetUserPasswordHandler(
	users out.UserRepository, hasher out.PasswordHasher,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.ResetUserPasswordHandler {
	if users == nil {
		panic("nil users")
	}
	if hasher == nil {
		panic("nil hasher")
	}
	return decorator.ApplyCommandDecorators[in.ResetUserPassword](
		resetUserPasswordHandler{users: users, hasher: hasher}, logger, metricsClient)
}

func (h resetUserPasswordHandler) Handle(ctx context.Context, cmd in.ResetUserPassword) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	password, err := user.NewPassword(cmd.NewPassword)
	if err != nil {
		return err
	}
	hash, err := h.hasher.Hash(password)
	if err != nil {
		return err
	}
	return h.users.UpdateUser(ctx, id, func(_ context.Context, u *user.User, _ out.UserPolicyReads) (*user.User, error) {
		if err := u.ChangePassword(hash); err != nil {
			return nil, err
		}
		return u, nil
	})
}
