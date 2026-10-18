// Package identity is job's adapter to the identity context (DEP-4, OUT-5,
// C9-02): it reads users through identity's UserDetails query.
package identity

import (
	"context"

	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// UserDirectory implements out.UserDirectory over identity's Application.
type UserDirectory struct {
	app identityin.Application
}

var _ out.UserDirectory = UserDirectory{}

func NewUserDirectory(app identityin.Application) UserDirectory {
	if app.Queries.UserDetails == nil {
		panic("nil identity application")
	}
	return UserDirectory{app: app}
}

// User reads the user on its own behalf: identity lets every user see
// themselves. Identity's errors keep their slugs (user-not-found, ...).
func (d UserDirectory) User(ctx context.Context, userID string) (in.UserView, error) {
	summary, err := d.app.Queries.UserDetails.Handle(ctx, identityin.UserDetails{
		Actor: identityin.Actor{UserID: userID, Role: remoteitem.RoleUser.String()}, UserID: userID,
	})
	if err != nil {
		return in.UserView{}, err
	}
	return in.UserView{
		ID: summary.ID, Username: summary.Username, Role: summary.Role, Status: summary.Status,
		PasswordLogin: summary.PasswordLogin, OIDCLinked: summary.OIDCLinked, CreatedAt: summary.CreatedAt,
	}, nil
}
