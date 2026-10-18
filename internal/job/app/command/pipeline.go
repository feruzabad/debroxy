package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// RefreshPipelineDependencies build the refresh pipeline.
type RefreshPipelineDependencies struct {
	Submission SubmissionDependencies
	Unmanaged  out.UnmanagedItemRepository
	SyncState  out.SyncStateRepository
	// AbsenceGrace returns the provider's absence grace (spec §5.6 step 7).
	AbsenceGrace func(remoteitem.ProviderID) time.Duration
	// Changes hears when a provider's back-off starts or ends; nil when no
	// Event Stream is wired (ADR 0021 rule 5).
	Changes out.ChangeNotifier
}

func (d RefreshPipelineDependencies) build(logger *slog.Logger) refreshPipeline {
	if d.Unmanaged == nil {
		panic("nil unmanagedItemRepository")
	}
	if d.SyncState == nil {
		panic("nil syncStateRepository")
	}
	if d.AbsenceGrace == nil {
		panic("nil absenceGrace")
	}
	return refreshPipeline{runner: d.Submission.build(logger), unmanaged: d.Unmanaged, syncState: d.SyncState, absenceGrace: d.AbsenceGrace}
}

// refreshPipeline is the single source of truth for provider state (spec
// §5.6): every step changes at most one item per transaction (D-11).
type refreshPipeline struct {
	runner       submissionRunner
	unmanaged    out.UnmanagedItemRepository
	syncState    out.SyncStateRepository
	absenceGrace func(remoteitem.ProviderID) time.Duration
}

// listedItems indexes the provider's stored items for the pipeline steps.
type listedItems struct {
	claimable   map[remoteitem.ProviderItemID]*remoteitem.RemoteItem
	submissions map[string]*remoteitem.RemoteItem // by provider hash
	revivable   map[remoteitem.ProviderItemID][]*remoteitem.RemoteItem
	deleting    map[remoteitem.ProviderItemID]bool
}

func indexItems(items []*remoteitem.RemoteItem) listedItems {
	index := listedItems{
		claimable: map[remoteitem.ProviderItemID]*remoteitem.RemoteItem{}, submissions: map[string]*remoteitem.RemoteItem{},
		revivable: map[remoteitem.ProviderItemID][]*remoteitem.RemoteItem{}, deleting: map[remoteitem.ProviderItemID]bool{},
	}
	for _, item := range items {
		_, submitting := item.Submission()
		switch state := item.State(); {
		case submitting:
			if item.ProviderHash() != "" {
				index.submissions[item.ProviderHash()] = item
			}
		case !item.HasProviderItemID():
		case state.IsClaimable():
			index.claimable[item.ProviderItemID()] = item
		case state == remoteitem.StateFailed, state == remoteitem.StateGone:
			index.revivable[item.ProviderItemID()] = append(index.revivable[item.ProviderItemID()], item)
		case state == remoteitem.StateDeleting:
			index.deleting[item.ProviderItemID()] = true
		}
	}
	return index
}

// run executes the seven steps. Only a complete listing applies absence and
// records an authoritative sync; an incomplete one reports errIncompleteListing.
func (p refreshPipeline) run(ctx context.Context, provider remoteitem.ProviderID) error {
	startedAt := p.runner.clock.Now()
	listing, err := p.runner.provider.ListRemoteItems(ctx, provider) // step 1
	if err != nil {
		return err
	}
	items, err := p.runner.items.RemoteItemsOfProvider(ctx, provider)
	if err != nil {
		return err
	}
	index := indexItems(items)
	seen := map[remoteitem.ProviderItemID]bool{}
	var leftovers []out.UnmanagedItem
	var errs []error
	for _, observation := range listing.Observations {
		pid := observation.ProviderItemID()
		seen[pid] = true
		if item, ok := index.claimable[pid]; ok { // step 2
			errs = append(errs, p.observe(ctx, item.ID(), observation))
			continue
		}
		if item, ok := index.submissions[observation.ProviderHash()]; ok && observation.ProviderHash() != "" { // step 3
			_, err := p.runner.applyOutcome(ctx, item.ID(), out.CreatedRemoteItem{ProviderItemID: pid, Observation: observation}, true)
			errs = append(errs, err)
			continue
		}
		if candidates := index.revivable[pid]; len(candidates) > 0 { // step 4
			if !observation.IsActiveOrReady() {
				continue // still failed at the provider: the item tracks it
			}
			revived, heldByDeleting, err := p.revive(ctx, newest(candidates), observation)
			errs = append(errs, err)
			if revived || heldByDeleting {
				continue
			}
		} else if index.deleting[pid] { // step 5
			continue
		}
		leftovers = append(leftovers, out.UnmanagedItem{ // step 6
			ProviderItemID: pid.String(), Kind: observation.Kind().String(), Name: observation.Name(),
			SizeBytes: observation.SizeBytes(), State: observation.State().String(),
		})
	}
	if len(leftovers) > 0 {
		errs = append(errs, p.unmanaged.RecordUnmanagedItems(ctx, provider, leftovers, startedAt))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if !listing.Complete {
		return errIncompleteListing
	}
	for pid, item := range index.claimable { // step 7
		if !seen[pid] {
			if err := p.markGone(ctx, item.ID(), pid, provider); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return p.syncState.RecordAuthoritativeSync(ctx, provider, startedAt)
}

func newest(items []*remoteitem.RemoteItem) *remoteitem.RemoteItem {
	return slices.MaxFunc(items, func(a, b *remoteitem.RemoteItem) int { return a.CreatedAt().Compare(b.CreatedAt()) })
}

// observe applies the observation to a known live item (step 2), unless it
// stopped being claimable or holding the ID meanwhile.
func (p refreshPipeline) observe(ctx context.Context, id remoteitem.ID, observation remoteitem.Observation) error {
	return p.runner.items.UpdateRemoteItem(ctx, id, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		if !item.State().IsClaimable() || item.ProviderItemID() != observation.ProviderItemID() {
			return item, nil
		}
		return item, item.Observe(observation)
	})
}

// revive brings back the newest failed or gone item holding the ID when no
// claimable item conflicts on any unique key, no deleting item holds the
// ID (C6-01), and the observation is later than every deletion of an item
// that held it (C7-01). heldByDeleting means the observation is dropped.
func (p refreshPipeline) revive(ctx context.Context, candidate *remoteitem.RemoteItem, observation remoteitem.Observation) (revived, heldByDeleting bool, err error) {
	reason := ""
	err = p.runner.items.UpdateRemoteItem(ctx, candidate.ID(), func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		revived, heldByDeleting, reason = false, false, ""
		if (item.State() != remoteitem.StateFailed && item.State() != remoteitem.StateGone) || item.ProviderItemID() != observation.ProviderItemID() {
			reason = "the item changed meanwhile"
			return item, nil
		}
		holders, err := reads.ItemsHoldingProviderItem(ctx, item.ProviderID(), item.ProviderItemID())
		if err != nil {
			return nil, err
		}
		for _, holder := range holders {
			switch {
			case holder.ID == item.ID():
			case holder.State == remoteitem.StateDeleting:
				heldByDeleting, reason = true, "a deleting item holds the provider item"
			case holder.State.IsClaimable():
				reason = "a claimable item holds the provider item"
			case !holder.DeletedAt.IsZero() && !observation.ObservedAt().After(holder.DeletedAt):
				reason = "the observation is not later than a deletion of the provider item"
			}
		}
		matching, err := reads.ClaimableItemsMatching(ctx, item.ProviderID(), item.Fingerprint(), item.ProviderHash())
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(matching, func(h out.ItemHolder) bool { return h.ID != item.ID() }) && reason == "" {
			reason = "a claimable item has the same content"
		}
		if reason != "" {
			return item, nil
		}
		revived = true
		return item, item.Revive(observation, p.runner.clock.Now())
	})
	if err == nil && !revived {
		p.runner.logger.InfoContext(ctx, "revive blocked", "remote_item_id", candidate.ID().String(), "reason", reason)
	}
	return revived, heldByDeleting, err
}

// markGone applies absence (step 7) outside the grace.
func (p refreshPipeline) markGone(ctx context.Context, id remoteitem.ID, pid remoteitem.ProviderItemID, provider remoteitem.ProviderID) error {
	grace := p.absenceGrace(provider)
	err := p.runner.items.UpdateRemoteItem(ctx, id, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		now := p.runner.clock.Now()
		if !item.State().IsClaimable() || item.ProviderItemID() != pid || item.IsWithinAbsenceGrace(now, grace) {
			return item, nil
		}
		return item, item.MarkGone(now)
	})
	if err != nil {
		return fmt.Errorf("mark %s gone: %w", id, err)
	}
	return nil
}
