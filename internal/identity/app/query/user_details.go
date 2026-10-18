package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type userDetailsHandler struct {
	readModel out.UserDetailsReadModel
}

func NewUserDetailsHandler(readModel out.UserDetailsReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.UserDetailsHandler {
	if readModel == nil {
		panic("nil readModel")
	}
	return decorator.ApplyQueryDecorators[in.UserDetails, in.UserSummary](userDetailsHandler{readModel: readModel}, logger, metricsClient)
}

func (h userDetailsHandler) Handle(ctx context.Context, query in.UserDetails) (in.UserSummary, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.UserSummary{}, err
	}
	userID, err := user.NewID(query.UserID)
	if err != nil {
		return in.UserSummary{}, err
	}
	if err := user.CanSeeUser(actor, userID); err != nil {
		return in.UserSummary{}, err
	}
	summary, err := h.readModel.UserDetails(ctx, userID.String())
	if err != nil {
		return in.UserSummary{}, err
	}
	if actor.Role != user.RoleAdmin {
		// The linked identity is shown in admin responses only (C14-05).
		summary.OIDCIdentity = nil
	}
	return summary, nil
}
