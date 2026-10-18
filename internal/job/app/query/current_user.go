package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type currentUserHandler struct {
	users out.UserDirectory
}

func NewCurrentUserHandler(users out.UserDirectory, logger *slog.Logger, metricsClient decorator.MetricsClient) in.CurrentUserHandler {
	if users == nil {
		panic("nil userDirectory")
	}
	return decorator.ApplyQueryDecorators[in.CurrentUser, in.UserView](currentUserHandler{users: users}, logger, metricsClient)
}

func (h currentUserHandler) Handle(ctx context.Context, query in.CurrentUser) (in.UserView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.UserView{}, err
	}
	return h.users.User(ctx, actor.UserID)
}
