package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/identity"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

type userDetails func(context.Context, identityin.UserDetails) (identityin.UserSummary, error)

func (f userDetails) Handle(ctx context.Context, q identityin.UserDetails) (identityin.UserSummary, error) {
	return f(ctx, q)
}

func TestUser_reads_the_user_on_its_own_behalf(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var asked identityin.UserDetails
	directory := identity.NewUserDirectory(identityin.Application{Queries: identityin.Queries{
		UserDetails: userDetails(func(_ context.Context, q identityin.UserDetails) (identityin.UserSummary, error) {
			asked = q
			return identityin.UserSummary{ID: "u1", Username: "alice", Role: "user", Status: "active", PasswordLogin: true, CreatedAt: created}, nil
		}),
	}})

	user, err := directory.User(context.Background(), "u1")

	require.NoError(t, err)
	assert.Equal(t, identityin.UserDetails{Actor: identityin.Actor{UserID: "u1", Role: "user"}, UserID: "u1"}, asked)
	assert.Equal(t, in.UserView{ID: "u1", Username: "alice", Role: "user", Status: "active", PasswordLogin: true, CreatedAt: created}, user)
}
