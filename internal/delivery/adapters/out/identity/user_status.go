// Package identity is delivery's adapter to the identity context (spec
// §3.2, DEP-4): it asks whether a user may still download.
package identity

import (
	"context"

	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// UserStatusChecker implements out.UserStatusChecker over identity's UserStatus query.
type UserStatusChecker struct {
	app identityin.Application
}

var _ out.UserStatusChecker = UserStatusChecker{}

func NewUserStatusChecker(app identityin.Application) UserStatusChecker {
	if app.Queries.UserStatus == nil {
		panic("nil identity application")
	}
	return UserStatusChecker{app: app}
}

func (c UserStatusChecker) IsActive(ctx context.Context, userID string) (bool, error) {
	status, err := c.app.Queries.UserStatus.Handle(ctx, identityin.UserStatus{UserID: userID})
	if err != nil {
		return false, err
	}
	return status.Exists && status.Active, nil
}
