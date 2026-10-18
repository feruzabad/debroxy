package out

import "context"

// UserStatusChecker is what delivery needs from identity (spec §3.2): a
// disabled or deleted user's links stop working.
type UserStatusChecker interface {
	IsActive(ctx context.Context, userID string) (bool, error)
}
