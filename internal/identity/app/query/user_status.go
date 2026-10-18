package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

type userStatusHandler struct {
	readModel out.UserStatusReadModel
}

func NewUserStatusHandler(readModel out.UserStatusReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.UserStatusHandler {
	if readModel == nil {
		panic("nil readModel")
	}
	return decorator.ApplyQueryDecorators[in.UserStatus, in.UserStatusResult](userStatusHandler{readModel: readModel}, logger, metricsClient)
}

func (h userStatusHandler) Handle(ctx context.Context, query in.UserStatus) (in.UserStatusResult, error) {
	return h.readModel.UserStatus(ctx, query.UserID)
}
