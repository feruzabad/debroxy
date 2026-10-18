package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// SQLiteJobStores implements the smaller job stores: slot quotas, the
// outbox, sync state, unmanaged items and the read models.
type SQLiteJobStores struct {
	database *commonsqlite.Database
}

func NewSQLiteJobStores(database *commonsqlite.Database) SQLiteJobStores {
	if database == nil {
		panic("nil database")
	}
	return SQLiteJobStores{database: database}
}

func (s SQLiteJobStores) write(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	return s.database.InTransaction(ctx, func(_ context.Context, tx *sql.Tx) error { return fn(sqlcgen.New(tx)) })
}

func (s SQLiteJobStores) read() *sqlcgen.Queries { return sqlcgen.New(s.database.Reader) }

func (s SQLiteJobStores) SetSlotQuota(ctx context.Context, quota slotquota.SlotQuota) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return q.UpsertSlotQuota(ctx, sqlcgen.UpsertSlotQuotaParams{UserID: quota.UserID(), MaxActiveJobs: int64(quota.MaxActiveJobs())})
	})
}

func (s SQLiteJobStores) DueMessages(ctx context.Context, now time.Time, limit int) ([]out.OutboxMessage, error) {
	rows, err := s.read().DueOutboxMessages(ctx, sqlcgen.DueOutboxMessagesParams{NextAttemptAt: toMillis(now), Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("due messages: %w", err)
	}
	messages := make([]out.OutboxMessage, 0, len(rows))
	for _, row := range rows {
		id, err := remoteitem.NewID(row.RemoteItemID)
		if err != nil {
			return nil, err
		}
		messages = append(messages, out.OutboxMessage{ID: row.ID, Kind: row.Kind, RemoteItemID: id, Attempts: int(row.Attempts), CreatedAt: fromMillis(row.CreatedAt)})
	}
	return messages, nil
}

func (s SQLiteJobStores) MarkProcessed(ctx context.Context, id int64, now time.Time) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return q.MarkOutboxMessageProcessed(ctx, sqlcgen.MarkOutboxMessageProcessedParams{ProcessedAt: toNullMillis(now), ID: id})
	})
}

func (s SQLiteJobStores) RecordFailure(ctx context.Context, id int64, nextAttemptAt time.Time, lastError string, park bool, now time.Time) error {
	parkedAt := sql.NullInt64{}
	if park {
		parkedAt = toNullMillis(now)
	}
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return q.RecordOutboxFailure(ctx, sqlcgen.RecordOutboxFailureParams{NextAttemptAt: toMillis(nextAttemptAt), LastError: lastError, ParkedAt: parkedAt, ID: id})
	})
}

func (s SQLiteJobStores) RecordAuthoritativeSync(ctx context.Context, provider remoteitem.ProviderID, at time.Time) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return q.UpsertSyncState(ctx, sqlcgen.UpsertSyncStateParams{ProviderID: provider.String(), LastAuthoritativeSyncAt: toMillis(at)})
	})
}

func (s SQLiteJobStores) LastAuthoritativeSync(ctx context.Context, provider remoteitem.ProviderID) (time.Time, error) {
	ms, err := s.read().SyncStateOf(ctx, provider.String())
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	return fromMillis(ms), err
}

// RecordUnmanagedItems upserts the leftovers of one listing.
func (s SQLiteJobStores) RecordUnmanagedItems(ctx context.Context, provider remoteitem.ProviderID, items []out.UnmanagedItem, seenAt time.Time) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		for _, item := range items {
			if err := q.UpsertUnmanagedItem(ctx, sqlcgen.UpsertUnmanagedItemParams{
				ProviderID: provider.String(), ProviderItemID: item.ProviderItemID, Kind: item.Kind, Name: item.Name,
				SizeBytes: item.SizeBytes, State: item.State, FirstSeenAt: toMillis(seenAt), LastSeenAt: toMillis(seenAt),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- read models (R-8) ---

func (s SQLiteJobStores) JobsOfUser(ctx context.Context, userID string) ([]in.JobView, error) {
	q := s.read()
	ids, err := q.ItemIDsOfOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	return viewsOf(ctx, q, ids, func(job remoteitem.Job) bool { return job.OwnerID() == userID })
}

func (s SQLiteJobStores) AllJobs(ctx context.Context) ([]in.JobView, error) {
	q := s.read()
	ids, err := q.ItemIDsWithJobs(ctx)
	if err != nil {
		return nil, err
	}
	return viewsOf(ctx, q, ids, func(remoteitem.Job) bool { return true })
}

func (s SQLiteJobStores) JobsOfItem(ctx context.Context, remoteItemID string) ([]in.JobView, error) {
	views, err := viewsOf(ctx, s.read(), []string{remoteItemID}, func(remoteitem.Job) bool { return true })
	var notFound remoteitem.NotFoundError
	if errors.As(err, &notFound) {
		return []in.JobView{}, nil
	}
	return views, err
}

func viewsOf(ctx context.Context, q *sqlcgen.Queries, itemIDs []string, include func(remoteitem.Job) bool) ([]in.JobView, error) {
	views := []in.JobView{}
	for _, id := range itemIDs {
		item, err := loadItemByID(ctx, q, id)
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

func (s SQLiteJobStores) JobDetails(ctx context.Context, jobID string) (in.JobView, error) {
	q := s.read()
	itemID, err := q.RemoteItemIDOfJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return in.JobView{}, remoteitem.JobNotFoundError{JobID: jobID}
	}
	if err != nil {
		return in.JobView{}, err
	}
	item, err := loadItemByID(ctx, q, itemID)
	if err != nil {
		return in.JobView{}, err
	}
	id, err := remoteitem.NewJobID(jobID)
	if err != nil {
		return in.JobView{}, err
	}
	job, ok := item.Job(id)
	if !ok {
		return in.JobView{}, remoteitem.JobNotFoundError{JobID: jobID}
	}
	return in.NewJobView(item, job), nil
}

func (s SQLiteJobStores) JobHistory(ctx context.Context, userID string) ([]in.HistoryEntry, error) {
	rows, err := s.read().HistoryOfOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	entries := make([]in.HistoryEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, historyEntry(row))
	}
	return entries, nil
}

func (s SQLiteJobStores) HistoryEntry(ctx context.Context, jobID string) (in.HistoryEntry, bool, error) {
	row, err := s.read().HistoryEntryByJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return in.HistoryEntry{}, false, nil
	}
	if err != nil {
		return in.HistoryEntry{}, false, err
	}
	return historyEntry(row), true, nil
}

func historyEntry(row sqlcgen.JobHistory) in.HistoryEntry {
	return in.HistoryEntry{
		JobID: row.JobID, DisplayName: row.DisplayName, ProviderID: row.ProviderID, Kind: row.Kind, Name: row.Name,
		FinalState: row.FinalState, FailureReason: row.FailureCode, OwnerID: row.OwnerID, OwnerUsername: row.OwnerUsername,
		Fingerprint: row.Fingerprint, ClaimedAt: fromMillis(row.ClaimedAt), ReleasedAt: fromMillis(row.ReleasedAt),
	}
}

func (s SQLiteJobStores) QuotaUsage(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.QuotaUsageResult, error) {
	q := s.read()
	used, err := q.CountActiveChargedItems(ctx, userID)
	if err != nil {
		return in.QuotaUsageResult{}, err
	}
	result := in.QuotaUsageResult{SlotsUsed: int(used), MaxActiveJobs: defaultMaxActiveJobs}
	row, err := q.SlotQuotaOf(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return in.QuotaUsageResult{}, err
	default:
		result.MaxActiveJobs = int(row.MaxActiveJobs)
	}
	return result, nil
}

// SlotQuotaOf reads the effective quota with the IsDefault flag (C14-01).
func (s SQLiteJobStores) SlotQuotaOf(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.SlotQuotaResult, error) {
	q := s.read()
	used, err := q.CountActiveChargedItems(ctx, userID)
	if err != nil {
		return in.SlotQuotaResult{}, err
	}
	result := in.SlotQuotaResult{SlotsUsed: int(used), MaxActiveJobs: defaultMaxActiveJobs, IsDefault: true}
	row, err := q.SlotQuotaOf(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return in.SlotQuotaResult{}, err
	default:
		result.MaxActiveJobs = int(row.MaxActiveJobs)
		result.IsDefault = false
	}
	return result, nil
}

// SubmitRateUsage reads the submit rate windows for both classes (spec §10.4).
func (s SQLiteJobStores) SubmitRateUsage(ctx context.Context, userID string, now time.Time) (in.SubmitRateUsageResult, error) {
	q := s.read()
	since := toMillis(now.Add(-time.Hour))
	var result in.SubmitRateUsageResult
	var err error
	if result.Uncached, err = s.submitRateClass(ctx, q, userID, "uncached", since); err != nil {
		return in.SubmitRateUsageResult{}, err
	}
	if result.Cached, err = s.submitRateClass(ctx, q, userID, "cached", since); err != nil {
		return in.SubmitRateUsageResult{}, err
	}
	return result, nil
}

func (s SQLiteJobStores) submitRateClass(ctx context.Context, q *sqlcgen.Queries, userID, class string, since int64) (in.SubmitRateClassUsage, error) {
	count, err := q.CountSubmitsSince(ctx, sqlcgen.CountSubmitsSinceParams{UserID: userID, Class: class, AcceptedAt: since})
	if err != nil {
		return in.SubmitRateClassUsage{}, err
	}
	usage := in.SubmitRateClassUsage{Used: int(count)}
	oldest, err := q.OldestSubmitSince(ctx, sqlcgen.OldestSubmitSinceParams{UserID: userID, Class: class, AcceptedAt: since})
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return in.SubmitRateClassUsage{}, err
	default:
		usage.Oldest = fromMillis(oldest)
	}
	return usage, nil
}

func (s SQLiteJobStores) UnmanagedItems(ctx context.Context) ([]in.UnmanagedItemView, error) {
	rows, err := s.read().UnmanagedItemList(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]in.UnmanagedItemView, 0, len(rows))
	for _, row := range rows {
		views = append(views, in.UnmanagedItemView{
			ProviderID: row.ProviderID, ProviderItemID: row.ProviderItemID, Kind: row.Kind, Name: row.Name,
			SizeBytes: row.SizeBytes, State: row.State, FirstSeenAt: fromMillis(row.FirstSeenAt), LastSeenAt: fromMillis(row.LastSeenAt),
		})
	}
	return views, nil
}

func (s SQLiteJobStores) ParkedOutboxMessages(ctx context.Context) ([]in.OutboxMessageView, error) {
	rows, err := s.read().ParkedOutboxMessageList(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]in.OutboxMessageView, 0, len(rows))
	for _, row := range rows {
		views = append(views, in.OutboxMessageView{
			ID: row.ID, Kind: row.Kind, RemoteItemID: row.RemoteItemID, Attempts: int(row.Attempts),
			LastError: row.LastError, CreatedAt: fromMillis(row.CreatedAt), ParkedAt: fromNullMillis(row.ParkedAt),
		})
	}
	return views, nil
}

// PruneExpiredRecords deletes old records in one transaction; files of
// pruned items go with them (ON DELETE CASCADE).
func (s SQLiteJobStores) PruneExpiredRecords(ctx context.Context, cutoffs out.RetentionCutoffs) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		steps := []func() (int64, error){
			func() (int64, error) { return q.PruneJobHistory(ctx, toMillis(cutoffs.JobHistoryReleasedBefore)) },
			func() (int64, error) { return q.PruneUnmanagedItems(ctx, toMillis(cutoffs.UnmanagedItemsSeenBefore)) },
			func() (int64, error) {
				return q.PruneProcessedOutboxMessages(ctx, toNullMillis(cutoffs.ProcessedOutboxBefore))
			},
			func() (int64, error) { return q.PruneDeletedRemoteItems(ctx, toNullMillis(cutoffs.DeletedItemsBefore)) },
			func() (int64, error) { return q.PruneSubmitRateWindows(ctx, toMillis(cutoffs.SubmitRateWindowsBefore)) },
		}
		for _, step := range steps {
			if _, err := step(); err != nil {
				return fmt.Errorf("prune: %w", err)
			}
		}
		return nil
	})
}
