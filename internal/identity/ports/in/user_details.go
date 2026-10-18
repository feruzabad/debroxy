package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// UserDetails returns one user: admins see everyone, users see themselves.
type UserDetails struct {
	Actor  Actor
	UserID string
}

type UserDetailsHandler decorator.QueryHandler[UserDetails, UserSummary]
