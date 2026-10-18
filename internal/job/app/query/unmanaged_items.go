package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type unmanagedItemsHandler struct {
	items out.UnmanagedItemsReadModel
}

func NewUnmanagedItemsHandler(items out.UnmanagedItemsReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.UnmanagedItemsHandler {
	if items == nil {
		panic("nil unmanagedItemsReadModel")
	}
	return decorator.ApplyQueryDecorators[in.UnmanagedItems, []in.UnmanagedItemView](unmanagedItemsHandler{items: items}, logger, metricsClient)
}

func (h unmanagedItemsHandler) Handle(ctx context.Context, query in.UnmanagedItems) ([]in.UnmanagedItemView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return nil, err
	}
	return h.items.UnmanagedItems(ctx)
}
