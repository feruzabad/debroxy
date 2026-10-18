package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type usersHandler struct {
	readModel out.UsersReadModel
}

func NewUsersHandler(readModel out.UsersReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.UsersHandler {
	if readModel == nil {
		panic("nil readModel")
	}
	return decorator.ApplyQueryDecorators[in.Users, []in.UserSummary](usersHandler{readModel: readModel}, logger, metricsClient)
}

func (h usersHandler) Handle(ctx context.Context, query in.Users) ([]in.UserSummary, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	if err := user.CanManageUsers(actor); err != nil {
		return nil, err
	}
	return h.readModel.Users(ctx)
}
