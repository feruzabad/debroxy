// Package command implements the identity commands.
package command

import (
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

func toDomainActor(actor in.Actor) (user.Actor, error) {
	userID, err := user.NewID(actor.UserID)
	if err != nil {
		return user.Actor{}, err
	}
	role, err := user.NewRoleFromString(actor.Role)
	if err != nil {
		return user.Actor{}, err
	}
	return user.Actor{UserID: userID, Role: role}, nil
}

// authorizeUserManagement checks the admin policy before any work is done.
func authorizeUserManagement(actor in.Actor) error {
	domainActor, err := toDomainActor(actor)
	if err != nil {
		return err
	}
	return user.CanManageUsers(domainActor)
}
