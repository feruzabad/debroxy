package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type parkedOutboxMessagesHandler struct {
	messages out.ParkedOutboxMessagesReadModel
}

func NewParkedOutboxMessagesHandler(messages out.ParkedOutboxMessagesReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.ParkedOutboxMessagesHandler {
	if messages == nil {
		panic("nil parkedOutboxMessagesReadModel")
	}
	return decorator.ApplyQueryDecorators[in.ParkedOutboxMessages, []in.OutboxMessageView](parkedOutboxMessagesHandler{messages: messages}, logger, metricsClient)
}

func (h parkedOutboxMessagesHandler) Handle(ctx context.Context, query in.ParkedOutboxMessages) ([]in.OutboxMessageView, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return nil, err
	}
	if err := remoteitem.CanActorAdminister(actor); err != nil {
		return nil, err
	}
	return h.messages.ParkedOutboxMessages(ctx)
}
