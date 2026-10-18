// Package user is the User aggregate of the identity context (spec §4.1).
package user

import (
	"errors"
	"time"
)

// User is a person allowed to use debroxy.
//
// Revoking sessions does not touch Session aggregates: every session records
// the user's session generation at login, and a session is valid only while
// that generation is current. Disabling the user or changing their password
// increments the generation, so one transaction changes one aggregate
// (ddd-rules D-11).
type User struct {
	id                ID
	username          Username
	passwordHash      PasswordHash
	oidcIdentity      OIDCIdentity
	role              Role
	status            Status
	sessionGeneration int
	createdAt         time.Time
	events            []Event
}

// NewUser creates an active user. passwordHash and oidcIdentity may each be
// zero, but not both.
func NewUser(id ID, username Username, role Role, passwordHash PasswordHash, oidcIdentity OIDCIdentity, now time.Time) (*User, error) {
	switch {
	case id.IsZero():
		return nil, errors.New("empty user ID")
	case username.IsZero():
		return nil, errors.New("empty username")
	case role.IsZero():
		return nil, errors.New("empty role")
	case now.IsZero():
		return nil, errors.New("zero creation time")
	case passwordHash.IsZero() && oidcIdentity.IsZero():
		return nil, ErrNoLoginMethod
	}
	u := &User{
		id:           id,
		username:     username,
		passwordHash: passwordHash,
		oidcIdentity: oidcIdentity,
		role:         role,
		status:       StatusActive,
		createdAt:    now,
	}
	u.record(UserCreated{UserID: id, Role: role})
	return u, nil
}

// UnmarshalUserFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalUserFromDatabase(
	id ID, username Username, role Role, status Status,
	passwordHash PasswordHash, oidcIdentity OIDCIdentity,
	sessionGeneration int, createdAt time.Time,
) (*User, error) {
	if status.IsZero() {
		return nil, errors.New("empty status")
	}
	if sessionGeneration < 0 {
		return nil, errors.New("negative session generation")
	}
	u, err := NewUser(id, username, role, passwordHash, oidcIdentity, createdAt)
	if err != nil {
		return nil, err
	}
	u.status = status
	u.sessionGeneration = sessionGeneration
	u.events = nil
	return u, nil
}

func (u *User) ID() ID                     { return u.id }
func (u *User) Username() Username         { return u.username }
func (u *User) PasswordHash() PasswordHash { return u.passwordHash }
func (u *User) OIDCIdentity() OIDCIdentity { return u.oidcIdentity }
func (u *User) Role() Role                 { return u.role }
func (u *User) Status() Status             { return u.status }
func (u *User) SessionGeneration() int     { return u.sessionGeneration }
func (u *User) CreatedAt() time.Time       { return u.createdAt }

func (u *User) IsActive() bool { return u.status == StatusActive }

// IsActiveAdmin reports whether the user counts toward the last-admin rule.
func (u *User) IsActiveAdmin() bool { return u.IsActive() && u.role == RoleAdmin }

// CanLogInWithPassword reports whether a password login may succeed at all.
func (u *User) CanLogInWithPassword() bool { return u.IsActive() && !u.passwordHash.IsZero() }

// CanLogInWithOIDC: only active users an admin linked beforehand (DEC-04).
func (u *User) CanLogInWithOIDC() bool { return u.IsActive() && !u.oidcIdentity.IsZero() }

// LinkOIDCIdentity sets the external identity. The caller checks in the
// same transaction that no other user holds it (ddd-rules R-10).
func (u *User) LinkOIDCIdentity(identity OIDCIdentity) error {
	if identity.IsZero() {
		return errors.New("empty OIDC identity")
	}
	if identity == u.oidcIdentity {
		return nil
	}
	u.oidcIdentity = identity
	u.record(OIDCIdentityLinked{UserID: u.id})
	return nil
}

// UnlinkOIDCIdentity clears the external identity; a user without a
// password would have no way to log in.
func (u *User) UnlinkOIDCIdentity() error {
	if u.oidcIdentity.IsZero() {
		return nil
	}
	if u.passwordHash.IsZero() {
		return ErrNoLoginMethod
	}
	u.oidcIdentity = OIDCIdentity{}
	u.record(OIDCIdentityUnlinked{UserID: u.id})
	return nil
}

// ChangeRole sets the role. activeAdmins is the number of active admins,
// this user included, read in the same transaction (ddd-rules R-10).
func (u *User) ChangeRole(role Role, activeAdmins int) error {
	if role.IsZero() {
		return errors.New("empty role")
	}
	if role == u.role {
		return nil
	}
	if u.IsActiveAdmin() && activeAdmins <= 1 {
		return ErrLastAdmin
	}
	u.role = role
	u.record(UserRoleChanged{UserID: u.id, Role: role})
	return nil
}

// Disable blocks logins and revokes every session. activeAdmins as in ChangeRole.
func (u *User) Disable(activeAdmins int) error {
	if !u.IsActive() {
		return nil
	}
	if u.IsActiveAdmin() && activeAdmins <= 1 {
		return ErrLastAdmin
	}
	u.status = StatusDisabled
	u.revokeSessions()
	u.record(UserDisabled{UserID: u.id})
	return nil
}

// Enable allows logins again. Revoked sessions stay revoked.
func (u *User) Enable() {
	if u.IsActive() {
		return
	}
	u.status = StatusActive
	u.record(UserEnabled{UserID: u.id})
}

// ChangePassword sets a new password hash and revokes every session; the
// caller may keep the session that made the change (spec §4.2).
func (u *User) ChangePassword(hash PasswordHash) error {
	if hash.IsZero() {
		return errors.New("empty password hash")
	}
	u.passwordHash = hash
	u.revokeSessions()
	u.record(UserPasswordChanged{UserID: u.id})
	return nil
}

func (u *User) revokeSessions() {
	u.sessionGeneration++
}

// Events returns the events recorded since the user was loaded (D-12).
func (u *User) Events() []Event { return u.events }

// ClearEvents is called by repositories after persisting.
func (u *User) ClearEvents() { u.events = nil }

func (u *User) record(e Event) { u.events = append(u.events, e) }
