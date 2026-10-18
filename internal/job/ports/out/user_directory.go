package out

import (
	"context"

	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// UserDirectory is what job needs from identity (C9-02, DEP-4): a user's
// own record, read on that user's behalf.
type UserDirectory interface {
	User(ctx context.Context, userID string) (in.UserView, error)
}
