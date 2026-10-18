// Package memory holds the in-memory job storage for tests and component
// tests (ddd-rules R-7, S-2). One mutex covers each whole load → reads →
// check → save section, so it races like SQLite's single writer (ADR 0002
// rule 12), and it enforces the partial unique indexes of ADR 0007.
package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

var errUniqueViolation = errors.New("unique index violation")

const uniqueRetries = 3

type outboxRow struct {
	message       out.OutboxMessage
	nextAttemptAt time.Time
	lastError     string
	parkedAt      time.Time
	processedAt   time.Time
}

type submitRow struct {
	userID, class string
	acceptedAt    time.Time
}

type unmanagedRow struct {
	view in.UnmanagedItemView
}

// MemoryJobStorage implements every job storage port.
type MemoryJobStorage struct {
	mu        sync.Mutex
	clock     clock.Clock
	items     map[remoteitem.ID]remoteitem.DatabaseFields
	history   []in.HistoryEntry
	quotas    map[string]slotquota.SlotQuota
	submits   []submitRow
	outbox    []*outboxRow
	nextID    int64
	syncs     map[string]time.Time
	unmanaged map[string]*unmanagedRow
	changes   out.ChangeNotifier
}

func NewMemoryJobStorage(clk clock.Clock) *MemoryJobStorage {
	if clk == nil {
		panic("nil clock")
	}
	return &MemoryJobStorage{
		clock: clk, items: map[remoteitem.ID]remoteitem.DatabaseFields{}, quotas: map[string]slotquota.SlotQuota{},
		syncs: map[string]time.Time{}, unmanaged: map[string]*unmanagedRow{},
	}
}

// NotifyChangesTo reports committed changes like the SQLite repository
// (ADR 0021 rule 5). Call it while wiring, before the storage is used.
func (s *MemoryJobStorage) NotifyChangesTo(changes out.ChangeNotifier) *MemoryJobStorage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes = changes
	return s
}

// --- RemoteItemRepository ---

func (s *MemoryJobStorage) JoinOrStart(
	ctx context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint,
	joinOrStartFn func(ctx context.Context, existing *remoteitem.RemoteItem, reads out.RemoteItemReads) (out.JoinOrStartResult, error),
) error {
	return s.withRetry(func() (*remoteitem.RemoteItem, error) {
		var existing *remoteitem.RemoteItem
		for _, fields := range s.items {
			if fields.ProviderID == provider && fields.Fingerprint == fingerprint && fields.State.IsClaimable() {
				item, err := remoteitem.UnmarshalRemoteItemFromDatabase(fields)
				if err != nil {
					return nil, err
				}
				existing = item
			}
		}
		result, err := joinOrStartFn(ctx, existing, memoryReads{s})
		if err != nil {
			return nil, err
		}
		if result.SubmitClass != "" {
			for _, job := range result.Item.Claims() {
				if job.Origin() == remoteitem.OriginCreated {
					s.submits = append(s.submits, submitRow{userID: job.OwnerID(), class: result.SubmitClass, acceptedAt: s.clock.Now()})
				}
			}
		}
		return result.Item, nil
	})
}

func (s *MemoryJobStorage) UpdateRemoteItem(
	ctx context.Context, id remoteitem.ID,
	updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error),
) error {
	return s.withRetry(func() (*remoteitem.RemoteItem, error) {
		item, err := s.load(id)
		if err != nil {
			return nil, err
		}
		return updateFn(ctx, item, memoryReads{s})
	})
}

func (s *MemoryJobStorage) UpdateRemoteItemByJob(
	ctx context.Context, jobID remoteitem.JobID, actor remoteitem.Actor,
	updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error),
) error {
	return s.withRetry(func() (*remoteitem.RemoteItem, error) {
		for id, fields := range s.items {
			for _, job := range fields.Claims {
				if job.ID() != jobID {
					continue
				}
				if !remoteitem.CanActorSeeJob(actor, job) {
					return nil, remoteitem.JobNotFoundError{JobID: jobID.String()}
				}
				item, err := s.load(id)
				if err != nil {
					return nil, err
				}
				return updateFn(ctx, item, memoryReads{s})
			}
		}
		return nil, remoteitem.JobNotFoundError{JobID: jobID.String()}
	})
}

func (s *MemoryJobStorage) RemoteItem(_ context.Context, id remoteitem.ID) (*remoteitem.RemoteItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

func (s *MemoryJobStorage) RemoteItemsOfProvider(_ context.Context, provider remoteitem.ProviderID) ([]*remoteitem.RemoteItem, error) {
	return s.filter(func(f remoteitem.DatabaseFields) bool {
		return f.ProviderID == provider && f.State != remoteitem.StateDeleted
	})
}

func (s *MemoryJobStorage) RemoteItemsInStates(_ context.Context, states ...remoteitem.State) ([]*remoteitem.RemoteItem, error) {
	return s.filter(func(f remoteitem.DatabaseFields) bool { return slices.Contains(states, f.State) })
}

func (s *MemoryJobStorage) filter(keep func(remoteitem.DatabaseFields) bool) ([]*remoteitem.RemoteItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []*remoteitem.RemoteItem
	for _, fields := range s.items {
		if keep(fields) {
			item, err := remoteitem.UnmarshalRemoteItemFromDatabase(fields)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt().Before(items[j].CreatedAt()) })
	return items, nil
}

func (s *MemoryJobStorage) load(id remoteitem.ID) (*remoteitem.RemoteItem, error) {
	fields, ok := s.items[id]
	if !ok {
		return nil, remoteitem.NotFoundError{ID: id.String()}
	}
	return remoteitem.UnmarshalRemoteItemFromDatabase(fields)
}

// withRetry holds the mutex for the whole section and retries after a
// unique violation, like the SQLite repository. The change is reported
// after the mutex is released, as after a commit.
func (s *MemoryJobStorage) withRetry(fn func() (*remoteitem.RemoteItem, error)) error {
	change, changes, err := s.saveWithRetry(fn)
	if err == nil && changes != nil {
		changes.ItemChanged(change)
	}
	return err
}

func (s *MemoryJobStorage) saveWithRetry(fn func() (*remoteitem.RemoteItem, error)) (out.ItemChange, out.ChangeNotifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 0; attempt < uniqueRetries; attempt++ {
		item, err := fn()
		if err != nil {
			return out.ItemChange{}, nil, err
		}
		err = s.save(item)
		if err == nil {
			change := itemChange(item)
			item.ClearEvents()
			return change, s.changes, nil
		}
		if !errors.Is(err, errUniqueViolation) {
			return out.ItemChange{}, nil, err
		}
	}
	return out.ItemChange{}, nil, fmt.Errorf("unique conflict persisted after %d attempts: %w", uniqueRetries, errUniqueViolation)
}

// itemChange names a saved item's change by IDs, before its events are cleared.
func itemChange(item *remoteitem.RemoteItem) out.ItemChange {
	before, after, released := item.ChangedOwners()
	change := out.ItemChange{RemoteItemID: item.ID(), ProviderID: item.ProviderID(), OwnersBefore: before, OwnersAfter: after}
	for _, job := range released {
		change.ReleasedJobs = append(change.ReleasedJobs, out.ReleasedJob{JobID: job.ID(), OwnerID: job.OwnerID()})
	}
	return change
}

func (s *MemoryJobStorage) save(item *remoteitem.RemoteItem) error {
	fields := fieldsOf(item)
	if fields.State.IsClaimable() {
		for id, other := range s.items {
			if id == fields.ID || other.ProviderID != fields.ProviderID || !other.State.IsClaimable() {
				continue
			}
			if other.Fingerprint == fields.Fingerprint ||
				(fields.ProviderHash != "" && other.ProviderHash == fields.ProviderHash) ||
				(!fields.ProviderItemID.IsZero() && other.ProviderItemID == fields.ProviderItemID) {
				return errUniqueViolation
			}
		}
	}
	for id, other := range s.items {
		if id == fields.ID {
			continue
		}
		for _, job := range other.Claims {
			for _, mine := range fields.Claims {
				if job.ID() == mine.ID() {
					return remoteitem.ErrDuplicateJobID
				}
			}
		}
	}
	now := s.clock.Now()
	for _, event := range item.Events() {
		switch e := event.(type) {
		case remoteitem.JobReleased:
			s.history = append(s.history, in.HistoryEntry{
				JobID: e.Job.ID().String(), DisplayName: e.Job.DisplayName(), ProviderID: fields.ProviderID.String(),
				Kind: fields.Kind.String(), Name: fields.Name, FinalState: e.FinalState.String(), FailureReason: e.FailureReason.Code(),
				OwnerID: e.Job.OwnerID(), OwnerUsername: e.Job.OwnerUsername(), Fingerprint: fields.Fingerprint.String(),
				ClaimedAt: e.Job.ClaimedAt(), ReleasedAt: now,
			})
		case remoteitem.DeletionRequested:
			s.nextID++
			s.outbox = append(s.outbox, &outboxRow{
				message:       out.OutboxMessage{ID: s.nextID, Kind: out.MessageKindDeleteRemoteItem, RemoteItemID: fields.ID, CreatedAt: now},
				nextAttemptAt: now,
			})
		}
	}
	s.items[fields.ID] = fields
	return nil
}

// fieldsOf copies every stored field of an item.
func fieldsOf(item *remoteitem.RemoteItem) remoteitem.DatabaseFields {
	fields := remoteitem.DatabaseFields{
		ID: item.ID(), ProviderID: item.ProviderID(), ProviderItemID: item.ProviderItemID(), ProviderHash: item.ProviderHash(),
		Kind: item.Kind(), Fingerprint: item.Fingerprint(), Source: item.Source(), Name: item.Name(), SizeBytes: item.SizeBytes(),
		State: item.State(), FailureReason: item.FailureReason(), Files: item.Files(), Bundle: item.Bundle(),
		PerFileDelivery: item.PerFileDelivery(), ExpiresAt: item.ExpiresAt(), Telemetry: item.Telemetry(),
		LastProgressAt: item.LastProgressAt(), LastObservationAt: item.LastObservationAt(), CreatedAt: item.CreatedAt(),
		DeletedAt: item.DeletedAt(), Claims: item.Claims(), ChargeHolder: item.ChargeHolder(), ChargeHolderReleased: item.ChargeHolderReleased(),
	}
	if submission, ok := item.Submission(); ok {
		fields.Submission = &submission
	}
	return fields
}

// memoryReads is used only while the storage mutex is held.
type memoryReads struct{ s *MemoryJobStorage }

func (r memoryReads) ActiveChargedItemCount(_ context.Context, userID string) (int, error) {
	count := 0
	for _, fields := range r.s.items {
		item, err := remoteitem.UnmarshalRemoteItemFromDatabase(fields)
		if err != nil {
			return 0, err
		}
		if item.IsChargedTo(userID) {
			count++
		}
	}
	return count, nil
}

func (r memoryReads) SlotQuota(_ context.Context, userID string) (slotquota.SlotQuota, bool, error) {
	quota, ok := r.s.quotas[userID]
	return quota, ok, nil
}

func (r memoryReads) SubmitCountSince(_ context.Context, userID, class string, since time.Time) (int, error) {
	count := 0
	for _, row := range r.s.submits {
		if row.userID == userID && row.class == class && row.acceptedAt.After(since) {
			count++
		}
	}
	return count, nil
}

func (r memoryReads) OldestSubmitSince(_ context.Context, userID, class string, since time.Time) (time.Time, error) {
	var oldest time.Time
	for _, row := range r.s.submits {
		if row.userID == userID && row.class == class && row.acceptedAt.After(since) {
			if oldest.IsZero() || row.acceptedAt.Before(oldest) {
				oldest = row.acceptedAt
			}
		}
	}
	return oldest, nil
}

func (r memoryReads) ItemsHoldingProviderItem(_ context.Context, provider remoteitem.ProviderID, providerItemID remoteitem.ProviderItemID) ([]out.ItemHolder, error) {
	var holders []out.ItemHolder
	for _, f := range r.s.items {
		if f.ProviderID == provider && f.ProviderItemID == providerItemID {
			holders = append(holders, out.ItemHolder{ID: f.ID, State: f.State, CreatedAt: f.CreatedAt, DeletedAt: f.DeletedAt})
		}
	}
	return holders, nil
}

func (r memoryReads) ClaimableItemsMatching(_ context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint, providerHash string) ([]out.ItemHolder, error) {
	var holders []out.ItemHolder
	for _, f := range r.s.items {
		if f.ProviderID == provider && f.State.IsClaimable() &&
			(f.Fingerprint == fingerprint || (providerHash != "" && f.ProviderHash == providerHash)) {
			holders = append(holders, out.ItemHolder{ID: f.ID, State: f.State, CreatedAt: f.CreatedAt, DeletedAt: f.DeletedAt})
		}
	}
	return holders, nil
}

func (r memoryReads) IsDeletionParked(_ context.Context, id remoteitem.ID) (bool, error) {
	for _, row := range r.s.outbox {
		if row.message.RemoteItemID == id && !row.parkedAt.IsZero() && row.processedAt.IsZero() {
			return true, nil
		}
	}
	return false, nil
}

// --- small stores ---

func (s *MemoryJobStorage) SetSlotQuota(_ context.Context, quota slotquota.SlotQuota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotas[quota.UserID()] = quota
	return nil
}

func (s *MemoryJobStorage) DueMessages(_ context.Context, now time.Time, limit int) ([]out.OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []out.OutboxMessage
	for _, row := range s.outbox {
		if row.processedAt.IsZero() && row.parkedAt.IsZero() && !row.nextAttemptAt.After(now) && len(due) < limit {
			due = append(due, row.message)
		}
	}
	return due, nil
}

func (s *MemoryJobStorage) MarkProcessed(_ context.Context, id int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.outbox {
		if row.message.ID == id {
			row.processedAt = now
		}
	}
	return nil
}

func (s *MemoryJobStorage) RecordFailure(_ context.Context, id int64, nextAttemptAt time.Time, lastError string, park bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.outbox {
		if row.message.ID == id {
			row.message.Attempts++
			row.nextAttemptAt, row.lastError = nextAttemptAt, lastError
			if park {
				row.parkedAt = now
			}
		}
	}
	return nil
}

func (s *MemoryJobStorage) RecordAuthoritativeSync(_ context.Context, provider remoteitem.ProviderID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs[provider.String()] = at
	return nil
}

func (s *MemoryJobStorage) LastAuthoritativeSync(_ context.Context, provider remoteitem.ProviderID) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncs[provider.String()], nil
}

func (s *MemoryJobStorage) RecordUnmanagedItems(_ context.Context, provider remoteitem.ProviderID, items []out.UnmanagedItem, seenAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		key := provider.String() + "/" + item.ProviderItemID
		row, ok := s.unmanaged[key]
		if !ok {
			row = &unmanagedRow{view: in.UnmanagedItemView{ProviderID: provider.String(), ProviderItemID: item.ProviderItemID, FirstSeenAt: seenAt}}
			s.unmanaged[key] = row
		}
		row.view.Kind, row.view.Name, row.view.SizeBytes, row.view.State, row.view.LastSeenAt = item.Kind, item.Name, item.SizeBytes, item.State, seenAt
	}
	return nil
}

// --- read models ---

func (s *MemoryJobStorage) jobViews(include func(remoteitem.Job) bool) ([]in.JobView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := []in.JobView{}
	for _, fields := range s.items {
		item, err := remoteitem.UnmarshalRemoteItemFromDatabase(fields)
		if err != nil {
			return nil, err
		}
		for _, job := range item.Claims() {
			if include(job) {
				views = append(views, in.NewJobView(item, job))
			}
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt.After(views[j].CreatedAt) })
	return views, nil
}

func (s *MemoryJobStorage) JobsOfUser(_ context.Context, userID string) ([]in.JobView, error) {
	return s.jobViews(func(job remoteitem.Job) bool { return job.OwnerID() == userID })
}

func (s *MemoryJobStorage) AllJobs(context.Context) ([]in.JobView, error) {
	return s.jobViews(func(remoteitem.Job) bool { return true })
}

func (s *MemoryJobStorage) JobsOfItem(_ context.Context, remoteItemID string) ([]in.JobView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := []in.JobView{}
	for id, fields := range s.items {
		if id.String() != remoteItemID {
			continue
		}
		item, err := remoteitem.UnmarshalRemoteItemFromDatabase(fields)
		if err != nil {
			return nil, err
		}
		for _, job := range item.Claims() {
			views = append(views, in.NewJobView(item, job))
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt.After(views[j].CreatedAt) })
	return views, nil
}

func (s *MemoryJobStorage) JobDetails(_ context.Context, jobID string) (in.JobView, error) {
	views, err := s.jobViews(func(job remoteitem.Job) bool { return job.ID().String() == jobID })
	if err != nil {
		return in.JobView{}, err
	}
	if len(views) == 0 {
		return in.JobView{}, remoteitem.JobNotFoundError{JobID: jobID}
	}
	return views[0], nil
}

func (s *MemoryJobStorage) JobHistory(_ context.Context, userID string) ([]in.HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := []in.HistoryEntry{}
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].OwnerID == userID {
			entries = append(entries, s.history[i])
		}
	}
	return entries, nil
}

func (s *MemoryJobStorage) HistoryEntry(_ context.Context, jobID string) (in.HistoryEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.history {
		if entry.JobID == jobID {
			return entry, true, nil
		}
	}
	return in.HistoryEntry{}, false, nil
}

func (s *MemoryJobStorage) QuotaUsage(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.QuotaUsageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reads := memoryReads{s}
	used, err := reads.ActiveChargedItemCount(ctx, userID)
	if err != nil {
		return in.QuotaUsageResult{}, err
	}
	result := in.QuotaUsageResult{SlotsUsed: used, MaxActiveJobs: defaultMaxActiveJobs}
	if quota, ok := s.quotas[userID]; ok {
		result.MaxActiveJobs = quota.MaxActiveJobs()
	}
	return result, nil
}

func (s *MemoryJobStorage) SlotQuotaOf(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.SlotQuotaResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reads := memoryReads{s}
	used, err := reads.ActiveChargedItemCount(ctx, userID)
	if err != nil {
		return in.SlotQuotaResult{}, err
	}
	result := in.SlotQuotaResult{SlotsUsed: used, MaxActiveJobs: defaultMaxActiveJobs, IsDefault: true}
	if quota, ok := s.quotas[userID]; ok {
		result.MaxActiveJobs = quota.MaxActiveJobs()
		result.IsDefault = false
	}
	return result, nil
}

func (s *MemoryJobStorage) SubmitRateUsage(_ context.Context, userID string, now time.Time) (in.SubmitRateUsageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	since := now.Add(-time.Hour)
	return in.SubmitRateUsageResult{
		Uncached: s.submitRateClass(userID, "uncached", since),
		Cached:   s.submitRateClass(userID, "cached", since),
	}, nil
}

func (s *MemoryJobStorage) submitRateClass(userID, class string, since time.Time) in.SubmitRateClassUsage {
	usage := in.SubmitRateClassUsage{}
	for _, row := range s.submits {
		if row.userID != userID || row.class != class || !row.acceptedAt.After(since) {
			continue
		}
		usage.Used++
		if usage.Oldest.IsZero() || row.acceptedAt.Before(usage.Oldest) {
			usage.Oldest = row.acceptedAt
		}
	}
	return usage
}

func (s *MemoryJobStorage) UnmanagedItems(context.Context) ([]in.UnmanagedItemView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := []in.UnmanagedItemView{}
	for _, row := range s.unmanaged {
		views = append(views, row.view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ProviderItemID < views[j].ProviderItemID })
	return views, nil
}

func (s *MemoryJobStorage) ParkedOutboxMessages(context.Context) ([]in.OutboxMessageView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := []in.OutboxMessageView{}
	for _, row := range s.outbox {
		if !row.parkedAt.IsZero() && row.processedAt.IsZero() {
			views = append(views, in.OutboxMessageView{
				ID: row.message.ID, Kind: row.message.Kind, RemoteItemID: row.message.RemoteItemID.String(),
				Attempts: row.message.Attempts, LastError: row.lastError, CreatedAt: row.message.CreatedAt, ParkedAt: row.parkedAt,
			})
		}
	}
	return views, nil
}

func (s *MemoryJobStorage) PruneExpiredRecords(_ context.Context, cutoffs out.RetentionCutoffs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = slices.DeleteFunc(s.history, func(e in.HistoryEntry) bool { return e.ReleasedAt.Before(cutoffs.JobHistoryReleasedBefore) })
	for key, row := range s.unmanaged {
		if row.view.LastSeenAt.Before(cutoffs.UnmanagedItemsSeenBefore) {
			delete(s.unmanaged, key)
		}
	}
	s.outbox = slices.DeleteFunc(s.outbox, func(row *outboxRow) bool {
		return !row.processedAt.IsZero() && row.processedAt.Before(cutoffs.ProcessedOutboxBefore)
	})
	for id, fields := range s.items {
		if fields.State == remoteitem.StateDeleted && fields.DeletedAt.Before(cutoffs.DeletedItemsBefore) {
			delete(s.items, id)
		}
	}
	s.submits = slices.DeleteFunc(s.submits, func(row submitRow) bool { return row.acceptedAt.Before(cutoffs.SubmitRateWindowsBefore) })
	return nil
}
