package user

// Event is a fact recorded by the User aggregate (ddd-rules D-12).
type Event interface{ isUserEvent() }

type UserCreated struct {
	UserID ID
	Role   Role
}

type UserRoleChanged struct {
	UserID ID
	Role   Role
}

type UserDisabled struct{ UserID ID }

type UserEnabled struct{ UserID ID }

// UserPasswordChanged also means every session was revoked.
type UserPasswordChanged struct{ UserID ID }

// OIDCIdentityLinked and OIDCIdentityUnlinked record admin changes of the
// external login (DEC-04).
type OIDCIdentityLinked struct{ UserID ID }

type OIDCIdentityUnlinked struct{ UserID ID }

func (UserCreated) isUserEvent()          {}
func (UserRoleChanged) isUserEvent()      {}
func (UserDisabled) isUserEvent()         {}
func (UserEnabled) isUserEvent()          {}
func (UserPasswordChanged) isUserEvent()  {}
func (OIDCIdentityLinked) isUserEvent()   {}
func (OIDCIdentityUnlinked) isUserEvent() {}
