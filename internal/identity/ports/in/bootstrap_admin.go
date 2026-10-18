package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// BootstrapAdmin creates the first admin. It succeeds only while no user exists.
type BootstrapAdmin struct {
	UserID   string
	Username string
	Password string
}

type BootstrapAdminHandler decorator.CommandHandler[BootstrapAdmin]
