package user

// CanManageUsers: only admins create, change, disable and reset users (spec §4.2, D-9).
func CanManageUsers(actor Actor) error {
	if actor.Role != RoleAdmin {
		return ErrForbidden
	}
	return nil
}

// CanSeeUser: admins see everyone; users see themselves. Others get
// NotFoundError, so user IDs do not leak.
func CanSeeUser(actor Actor, userID ID) error {
	if actor.Role == RoleAdmin || actor.UserID == userID {
		return nil
	}
	return NotFoundError{ID: userID.String()}
}
