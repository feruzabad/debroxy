package remoteitem

import commonerrors "github.com/feruzabad/debroxy/internal/common/errors"

// CanActorSeeJob: the owner or an admin (spec §5.3). Repositories enforce
// it before updateFn (R-3); everyone else gets job-not-found (NFR-04).
func CanActorSeeJob(actor Actor, job Job) bool {
	return actor.IsAdmin() || actor.UserID == job.ownerID
}

// ErrAdminOnly: the action needs the admin role.
var ErrAdminOnly = commonerrors.NewAuthorizationError("only admins may do this", "forbidden")

// CanActorAdminister: only admins see every job, set slot quotas and read
// the operator lists (spec §5.4).
func CanActorAdminister(actor Actor) error {
	if actor.IsAdmin() {
		return nil
	}
	return ErrAdminOnly
}
