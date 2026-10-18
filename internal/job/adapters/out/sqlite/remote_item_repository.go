package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// uniqueRetries bounds retries after a unique-index violation (spec §5.5 step 6).
const uniqueRetries = 3

type SQLiteRemoteItemRepository struct {
	database *commonsqlite.Database
	clock    clock.Clock
	changes  out.ChangeNotifier
}

// NewSQLiteRemoteItemRepository reports committed changes to changes, which
// may be nil when no Event Stream is wired (ADR 0021 rule 5).
func NewSQLiteRemoteItemRepository(database *commonsqlite.Database, clk clock.Clock, changes out.ChangeNotifier) SQLiteRemoteItemRepository {
	if database == nil {
		panic("nil database")
	}
	if clk == nil {
		panic("nil clock")
	}
	return SQLiteRemoteItemRepository{database: database, clock: clk, changes: changes}
}

func (r SQLiteRemoteItemRepository) JoinOrStart(
	ctx context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint,
	joinOrStartFn func(ctx context.Context, existing *remoteitem.RemoteItem, reads out.RemoteItemReads) (out.JoinOrStartResult, error),
) error {
	return r.withRetry(ctx, func(ctx context.Context, q *sqlcgen.Queries) (*remoteitem.RemoteItem, error) {
		var existing *remoteitem.RemoteItem
		row, err := q.ClaimableItemByFingerprint(ctx, sqlcgen.ClaimableItemByFingerprintParams{ProviderID: provider.String(), Fingerprint: fingerprint.String()})
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, fmt.Errorf("look up item: %w", err)
		default:
			if existing, err = loadItem(ctx, q, row); err != nil {
				return nil, err
			}
		}
		result, err := joinOrStartFn(ctx, existing, sqliteReads{queries: q})
		if err != nil {
			return nil, err
		}
		if result.SubmitClass != "" {
			if err := q.InsertSubmitWindow(ctx, sqlcgen.InsertSubmitWindowParams{
				UserID: submitter(result.Item), Class: result.SubmitClass, AcceptedAt: toMillis(r.clock.Now()),
			}); err != nil {
				return nil, fmt.Errorf("record submit: %w", err)
			}
		}
		return result.Item, nil
	})
}

// submitter is the creator of a freshly started item.
func submitter(item *remoteitem.RemoteItem) string {
	for _, job := range item.Claims() {
		if job.Origin() == remoteitem.OriginCreated {
			return job.OwnerID()
		}
	}
	return ""
}

func (r SQLiteRemoteItemRepository) UpdateRemoteItem(
	ctx context.Context, id remoteitem.ID,
	updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error),
) error {
	return r.withRetry(ctx, func(ctx context.Context, q *sqlcgen.Queries) (*remoteitem.RemoteItem, error) {
		item, err := loadItemByID(ctx, q, id.String())
		if err != nil {
			return nil, err
		}
		return updateFn(ctx, item, sqliteReads{queries: q})
	})
}

func (r SQLiteRemoteItemRepository) UpdateRemoteItemByJob(
	ctx context.Context, jobID remoteitem.JobID, actor remoteitem.Actor,
	updateFn func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error),
) error {
	return r.withRetry(ctx, func(ctx context.Context, q *sqlcgen.Queries) (*remoteitem.RemoteItem, error) {
		itemID, err := q.RemoteItemIDOfJob(ctx, jobID.String())
		if errors.Is(err, sql.ErrNoRows) {
			return nil, remoteitem.JobNotFoundError{JobID: jobID.String()}
		}
		if err != nil {
			return nil, fmt.Errorf("find job: %w", err)
		}
		item, err := loadItemByID(ctx, q, itemID)
		if err != nil {
			return nil, err
		}
		job, ok := item.Job(jobID)
		if !ok || !remoteitem.CanActorSeeJob(actor, job) {
			return nil, remoteitem.JobNotFoundError{JobID: jobID.String()}
		}
		return updateFn(ctx, item, sqliteReads{queries: q})
	})
}

func (r SQLiteRemoteItemRepository) RemoteItem(ctx context.Context, id remoteitem.ID) (*remoteitem.RemoteItem, error) {
	return loadItemByID(ctx, sqlcgen.New(r.database.Reader), id.String())
}

func (r SQLiteRemoteItemRepository) RemoteItemsOfProvider(ctx context.Context, provider remoteitem.ProviderID) ([]*remoteitem.RemoteItem, error) {
	q := sqlcgen.New(r.database.Reader)
	rows, err := q.RemoteItemsOfProvider(ctx, provider.String())
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return loadItems(ctx, q, rows)
}

func (r SQLiteRemoteItemRepository) RemoteItemsInStates(ctx context.Context, states ...remoteitem.State) ([]*remoteitem.RemoteItem, error) {
	names := make([]string, 0, len(states))
	for _, s := range states {
		names = append(names, s.String())
	}
	q := sqlcgen.New(r.database.Reader)
	rows, err := q.RemoteItemsInStates(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return loadItems(ctx, q, rows)
}

// withRetry runs fn in a write transaction, saves the item it returns with
// its events, and retries from the start after a unique-index violation.
// After the commit, and only then, it reports the change.
func (r SQLiteRemoteItemRepository) withRetry(ctx context.Context, fn func(ctx context.Context, q *sqlcgen.Queries) (*remoteitem.RemoteItem, error)) error {
	var err error
	for attempt := 0; attempt < uniqueRetries; attempt++ {
		var saved *remoteitem.RemoteItem
		err = r.database.InTransaction(ctx, func(ctx context.Context, tx *sql.Tx) error {
			q := sqlcgen.New(tx)
			item, err := fn(ctx, q)
			if err != nil {
				return err
			}
			if err := r.save(ctx, q, item); err != nil {
				return err
			}
			saved = item
			return nil
		})
		if err == nil {
			if r.changes != nil {
				r.changes.ItemChanged(itemChange(saved))
			}
			saved.ClearEvents()
			return nil
		}
		if !commonsqlite.IsUniqueViolation(err) {
			return err
		}
	}
	return fmt.Errorf("unique conflict persisted after %d attempts: %w", uniqueRetries, err)
}

// save writes the item, its files and claims, and the storage effects of
// its events (R-6).
func (r SQLiteRemoteItemRepository) save(ctx context.Context, q *sqlcgen.Queries, item *remoteitem.RemoteItem) error {
	params, err := marshalRemoteItem(item)
	if err != nil {
		return err
	}
	if err := q.UpsertRemoteItem(ctx, params); err != nil {
		return fmt.Errorf("save item: %w", err)
	}
	if err := q.DeleteFilesOfItem(ctx, params.ID); err != nil {
		return err
	}
	for position, f := range item.Files() {
		if err := q.InsertRemoteFile(ctx, sqlcgen.InsertRemoteFileParams{
			RemoteItemID: params.ID, Position: int64(position), FileID: f.FileID(), Path: f.Path(), SizeBytes: f.SizeBytes(),
		}); err != nil {
			return fmt.Errorf("save file: %w", err)
		}
	}
	if err := q.DeleteJobsOfItem(ctx, params.ID); err != nil {
		return err
	}
	for _, job := range item.Claims() {
		if err := q.InsertJob(ctx, sqlcgen.InsertJobParams{
			ID: job.ID().String(), RemoteItemID: params.ID, OwnerID: job.OwnerID(), OwnerUsername: job.OwnerUsername(),
			DisplayName: job.DisplayName(), ClaimedAt: toMillis(job.ClaimedAt()), Origin: job.Origin().String(),
		}); err != nil {
			if commonsqlite.IsUniqueViolation(err) && strings.Contains(err.Error(), "jobs.id") {
				return remoteitem.ErrDuplicateJobID // a concurrent request used the same JobID (DEC-38)
			}
			return fmt.Errorf("save job: %w", err)
		}
	}
	now := toMillis(r.clock.Now())
	for _, event := range item.Events() {
		switch e := event.(type) {
		case remoteitem.JobReleased:
			if err := q.InsertHistoryEntry(ctx, sqlcgen.InsertHistoryEntryParams{
				JobID: e.Job.ID().String(), OwnerID: e.Job.OwnerID(), OwnerUsername: e.Job.OwnerUsername(),
				DisplayName: e.Job.DisplayName(), RemoteItemID: params.ID, ProviderID: params.ProviderID,
				Kind: params.Kind, Fingerprint: params.Fingerprint, Name: item.Name(), FinalState: e.FinalState.String(),
				FailureCode: e.FailureReason.Code(), FailureDetail: e.FailureReason.Detail(),
				ClaimedAt: toMillis(e.Job.ClaimedAt()), ReleasedAt: now,
			}); err != nil {
				return fmt.Errorf("record history: %w", err)
			}
		case remoteitem.DeletionRequested:
			if err := q.InsertOutboxMessage(ctx, sqlcgen.InsertOutboxMessageParams{
				Kind: out.MessageKindDeleteRemoteItem, RemoteItemID: params.ID, CreatedAt: now, NextAttemptAt: now,
			}); err != nil {
				return fmt.Errorf("record outbox message: %w", err)
			}
		}
	}
	return nil
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

func loadItemByID(ctx context.Context, q *sqlcgen.Queries, id string) (*remoteitem.RemoteItem, error) {
	row, err := q.RemoteItemByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, remoteitem.NotFoundError{ID: id}
	}
	if err != nil {
		return nil, fmt.Errorf("load item: %w", err)
	}
	return loadItem(ctx, q, row)
}

func loadItem(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.RemoteItem) (*remoteitem.RemoteItem, error) {
	files, err := q.FilesOfItem(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("load files: %w", err)
	}
	jobs, err := q.JobsOfItem(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	return unmarshalRemoteItem(row, files, jobs)
}

func loadItems(ctx context.Context, q *sqlcgen.Queries, rows []sqlcgen.RemoteItem) ([]*remoteitem.RemoteItem, error) {
	items := make([]*remoteitem.RemoteItem, 0, len(rows))
	for _, row := range rows {
		item, err := loadItem(ctx, q, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// sqliteReads runs inside the write transaction (R-10).
type sqliteReads struct {
	queries *sqlcgen.Queries
}

func (r sqliteReads) ActiveChargedItemCount(ctx context.Context, userID string) (int, error) {
	count, err := r.queries.CountActiveChargedItems(ctx, userID)
	return int(count), err
}

func (r sqliteReads) SlotQuota(ctx context.Context, userID string) (slotquota.SlotQuota, bool, error) {
	row, err := r.queries.SlotQuotaOf(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return slotquota.SlotQuota{}, false, nil
	}
	if err != nil {
		return slotquota.SlotQuota{}, false, err
	}
	quota, err := slotquota.NewSlotQuota(row.UserID, int(row.MaxActiveJobs))
	return quota, err == nil, err
}

func (r sqliteReads) SubmitCountSince(ctx context.Context, userID, class string, since time.Time) (int, error) {
	count, err := r.queries.CountSubmitsSince(ctx, sqlcgen.CountSubmitsSinceParams{UserID: userID, Class: class, AcceptedAt: toMillis(since)})
	return int(count), err
}

func (r sqliteReads) OldestSubmitSince(ctx context.Context, userID, class string, since time.Time) (time.Time, error) {
	millis, err := r.queries.OldestSubmitSince(ctx, sqlcgen.OldestSubmitSinceParams{UserID: userID, Class: class, AcceptedAt: toMillis(since)})
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return fromMillis(millis), nil
}

func (r sqliteReads) ItemsHoldingProviderItem(ctx context.Context, provider remoteitem.ProviderID, providerItemID remoteitem.ProviderItemID) ([]out.ItemHolder, error) {
	rows, err := r.queries.ItemsHoldingProviderItem(ctx, sqlcgen.ItemsHoldingProviderItemParams{
		ProviderID: provider.String(), ProviderItemID: toNullString(providerItemID.String()),
	})
	if err != nil {
		return nil, err
	}
	holders := make([]out.ItemHolder, 0, len(rows))
	for _, row := range rows {
		holder, err := itemHolder(row.ID, row.State, row.CreatedAt, row.DeletedAt)
		if err != nil {
			return nil, err
		}
		holders = append(holders, holder)
	}
	return holders, nil
}

func (r sqliteReads) ClaimableItemsMatching(ctx context.Context, provider remoteitem.ProviderID, fingerprint remoteitem.Fingerprint, providerHash string) ([]out.ItemHolder, error) {
	rows, err := r.queries.ClaimableItemsMatching(ctx, sqlcgen.ClaimableItemsMatchingParams{
		ProviderID: provider.String(), Fingerprint: fingerprint.String(), ProviderHash: providerHash,
	})
	if err != nil {
		return nil, err
	}
	holders := make([]out.ItemHolder, 0, len(rows))
	for _, row := range rows {
		holder, err := itemHolder(row.ID, row.State, row.CreatedAt, row.DeletedAt)
		if err != nil {
			return nil, err
		}
		holders = append(holders, holder)
	}
	return holders, nil
}

func (r sqliteReads) IsDeletionParked(ctx context.Context, id remoteitem.ID) (bool, error) {
	count, err := r.queries.CountParkedDeletions(ctx, id.String())
	return count > 0, err
}

func itemHolder(id, state string, createdAt int64, deletedAt sql.NullInt64) (out.ItemHolder, error) {
	itemID, err := remoteitem.NewID(id)
	if err != nil {
		return out.ItemHolder{}, err
	}
	itemState, err := remoteitem.NewStateFromString(state)
	if err != nil {
		return out.ItemHolder{}, err
	}
	return out.ItemHolder{ID: itemID, State: itemState, CreatedAt: fromMillis(createdAt), DeletedAt: fromNullMillis(deletedAt)}, nil
}
