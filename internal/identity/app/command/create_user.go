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

type createUserHandler struct {
	users  out.UserRepository
	hasher out.PasswordHasher
	clock  clock.Clock
}

func NewCreateUserHandler(
	users out.UserRepository, hasher out.PasswordHasher, clock clock.Clock,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.CreateUserHandler {
	if users == nil {
		panic("nil users")
	}
	if hasher == nil {
		panic("nil hasher")
	}
	if clock == nil {
		panic("nil clock")
	}
	return decorator.ApplyCommandDecorators[in.CreateUser](
		createUserHandler{users: users, hasher: hasher, clock: clock}, logger, metricsClient)
}

func (h createUserHandler) Handle(ctx context.Context, cmd in.CreateUser) error {
	if err := authorizeUserManagement(cmd.Actor); err != nil {
		return err
	}
	id, err := user.NewID(cmd.UserID)
	if err != nil {
		return err
	}
	username, err := user.NewUsername(cmd.Username)
	if err != nil {
		return err
	}
	role, err := user.NewRoleFromString(cmd.Role)
	if err != nil {
		return err
	}
	hash, err := h.initialPasswordHash(cmd.Password)
	if err != nil {
		return err
	}
	identity := user.OIDCIdentity{}
	if cmd.OIDCIssuer != "" || cmd.OIDCSubject != "" {
		if identity, err = user.NewOIDCIdentity(cmd.OIDCIssuer, cmd.OIDCSubject); err != nil {
			return err
		}
	}
	return h.users.CreateUser(ctx, func(ctx context.Context, reads out.UserPolicyReads) (*user.User, error) {
		if !identity.IsZero() {
			if err := ensureOIDCIdentityFree(ctx, reads, identity, id); err != nil {
				return nil, err
			}
		}
		return user.NewUser(id, username, role, hash, identity, h.clock.Now())
	})
}

// initialPasswordHash: no password means an OIDC-only user (spec §4.2).
func (h createUserHandler) initialPasswordHash(plaintext string) (user.PasswordHash, error) {
	if plaintext == "" {
		return user.PasswordHash{}, nil
	}
	password, err := user.NewPassword(plaintext)
	if err != nil {
		return user.PasswordHash{}, err
	}
	return h.hasher.Hash(password)
}
