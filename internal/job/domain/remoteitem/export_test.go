package remoteitem

// CheckInvariants exposes the invariant check to the external tests.
func CheckInvariants(r *RemoteItem) error { return r.checkInvariants() }
