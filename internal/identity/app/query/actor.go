// Package query implements the identity queries.
package query

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
