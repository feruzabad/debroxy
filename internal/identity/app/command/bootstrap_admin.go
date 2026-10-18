package command

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type bootstrapAdminHandler struct {
	users  out.UserRepository
	hasher out.PasswordHasher
	clock  clock.Clock
}

func NewBootstrapAdminHandler(
	users out.UserRepository, hasher out.PasswordHasher, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.BootstrapAdminHandler {
	if users == nil {
		panic("nil users")
	}
	if hasher == nil {
		panic("nil hasher")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.BootstrapAdmin](
		bootstrapAdminHandler{users: users, hasher: hasher, clock: clock}, logger, metricsClient)
}

func (h bootstrapAdminHandler) Handle(ctx context.Context, cmd in.BootstrapAdmin) error {
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	username, err := user.NewUsername(cmd.Username)
	if err != nil {
		return err
	}
	password, err := user.NewPassword(cmd.Password)
	if err != nil {
		return err
	}
	hash, err := h.hasher.Hash(password)
	if err != nil {
		return err
	}
	return h.users.CreateUser(ctx, func(ctx context.Context, reads out.UserPolicyReads) (*user.User, error) {
		count, err := reads.UserCount(ctx)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, user.ErrBootstrapNotAllowed
		}
		return user.NewUser(id, username, user.RoleAdmin, hash, user.OIDCIdentity{}, h.clock.Now())
	})
}
