// Package query implements the job queries.
package query

import (
	"errors"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

func toDomainActor(actor in.Actor) (remoteitem.Actor, error) {
	role, err := remoteitem.NewRoleFromString(actor.Role)
	if err != nil {
		return remoteitem.Actor{}, err
	}
	if actor.UserID == "" {
		return remoteitem.Actor{}, errors.New("actor without user ID")
	}
	return remoteitem.Actor{UserID: actor.UserID, Role: role}, nil
}

// visibleJob applies CanActorSeeJob to a view: a job the actor may not see
// is reported as not found (R-3, NFR-04).
func visibleJob(actor remoteitem.Actor, view in.JobView) error {
	if actor.IsAdmin() || actor.UserID == view.OwnerID {
		return nil
	}
	return remoteitem.JobNotFoundError{JobID: view.ID}
}
