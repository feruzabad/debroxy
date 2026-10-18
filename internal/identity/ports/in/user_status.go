package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// UserStatus tells other contexts whether a user may still act (delivery
// checks it on every download).
type UserStatus struct {
	UserID string
}

type UserStatusResult struct {
	Exists bool
	Active bool
}

type UserStatusHandler decorator.QueryHandler[UserStatus, UserStatusResult]
