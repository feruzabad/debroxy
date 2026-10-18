package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// OutboxConfig holds the retry policy of ADR 0008.
type OutboxConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DeletionDependencies build the deletion executor shared by
// ExecuteRemoteItemDeletion and ProcessOutbox.
type DeletionDependencies struct {
	Items    out.RemoteItemRepository
	Outbox   out.OutboxRepository
	Provider out.ProviderGateway
	Clock    clock.Clock
	Config   OutboxConfig
}

func (d DeletionDependencies) build(logger *slog.Logger) deletionExecutor {
	switch {
	case d.Items == nil:
		panic("nil remoteItemRepository")
	case d.Outbox == nil:
		panic("nil outboxRepository")
	case d.Provider == nil:
		panic("nil providerGateway")
	case d.Clock == nil:
		panic("nil clock")
	case logger == nil:
		panic("nil logger")
	}
	return deletionExecutor{items: d.Items, outbox: d.Outbox, provider: d.Provider, clock: d.Clock, config: d.Config, logger: logger}
}

// deletionExecutor runs ExecuteRemoteItemDeletion (spec §5.7). It never
// holds a transaction across the provider call.
type deletionExecutor struct {
	items    out.RemoteItemRepository
	outbox   out.OutboxRepository
	provider out.ProviderGateway
	clock    clock.Clock
	config   OutboxConfig
	logger   *slog.Logger
}

// run executes one message and records its result on the message.
func (e deletionExecutor) run(ctx context.Context, message out.OutboxMessage) error {
	err := e.delete(ctx, message.RemoteItemID)
	now := e.clock.Now()
	if err == nil {
		return e.outbox.MarkProcessed(ctx, message.ID, now)
	}
	attempts := message.Attempts + 1
	park := attempts >= e.config.MaxAttempts
	if park {
		// Parking also fails the submissions deferred behind this item;
		// ExpireSubmissions does that, one item per transaction (C5-04).
		e.logger.ErrorContext(ctx, "outbox message parked", "message_id", message.ID, "remote_item_id", message.RemoteItemID.String(), "attempts", attempts, "error", err.Error())
	}
	return e.outbox.RecordFailure(ctx, message.ID, now.Add(e.backoff(attempts)), err.Error(), park, now)
}

// backoff doubles from the initial back-off up to the maximum (1 s … 1 h).
func (e deletionExecutor) backoff(attempts int) time.Duration {
	backoff := e.config.InitialBackoff << min(attempts-1, 40)
	if backoff <= 0 || backoff > e.config.MaxBackoff {
		return e.config.MaxBackoff
	}
	return backoff
}

// delete: transaction 1 supersedes or picks the target, the provider call
// deletes (verified by a look-up on error), transaction 2 confirms.
func (e deletionExecutor) delete(ctx context.Context, id remoteitem.ID) error {
	var (
		pending  bool
		provider remoteitem.ProviderID
		kind     remoteitem.Kind
		pid      remoteitem.ProviderItemID
	)
	err := e.items.UpdateRemoteItem(ctx, id, func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		pending = false
		if item.State() != remoteitem.StateDeleting || !item.HasProviderItemID() {
			return item, nil // nothing to delete any more: the message is done
		}
		holders, err := reads.ItemsHoldingProviderItem(ctx, item.ProviderID(), item.ProviderItemID())
		if err != nil {
			return nil, err
		}
		if claimableHolder(item.ID(), holders) {
			// A new submission took the provider item over (ADR 0007 rule 7).
			return item, item.SupersedeDeletion(e.clock.Now())
		}
		pending, provider, kind, pid = true, item.ProviderID(), item.Kind(), item.ProviderItemID()
		return item, nil
	})
	var notFound remoteitem.NotFoundError
	if errors.As(err, &notFound) {
		return nil // pruned meanwhile
	}
	if err != nil || !pending {
		return err
	}

	if err := e.provider.DeleteRemoteItem(ctx, provider, kind, pid); err != nil {
		return err
	}

	return e.items.UpdateRemoteItem(ctx, id, func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		if item.State() != remoteitem.StateDeleting {
			return item, nil
		}
		holders, err := reads.ItemsHoldingProviderItem(ctx, item.ProviderID(), item.ProviderItemID())
		if err != nil {
			return nil, err
		}
		if claimableHolder(item.ID(), holders) {
			// Cannot happen unless the provider reuses item IDs; the next
			// refresh marks that item gone (spec §5.7 step 3).
			e.logger.WarnContext(ctx, "claimable item holds a deleted provider item", "remote_item_id", item.ID().String(), "provider_item_id", pid.String())
		}
		return item, item.ConfirmDeleted(e.clock.Now())
	})
}

func claimableHolder(self remoteitem.ID, holders []out.ItemHolder) bool {
	for _, holder := range holders {
		if holder.ID != self && holder.State.IsClaimable() {
			return true
		}
	}
	return false
}
