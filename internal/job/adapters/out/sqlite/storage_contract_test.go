package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/id"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
	"github.com/feruzabad/debroxy/migrations"
)

var t0 = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

type storage struct {
	name     string
	items    out.RemoteItemRepository
	quotas   out.SlotQuotaRepository
	outbox   out.OutboxRepository
	syncs    out.SyncStateRepository
	leftover out.UnmanagedItemRepository
	jobs     out.JobReadModel
	clock    *clock.Fixed
	prune    out.RetentionRepository
	changes  *changeRecorder
}

// changeRecorder records the changes a repository reports (ADR 0021 rule 5).
type changeRecorder struct {
	mu    sync.Mutex
	items []out.ItemChange
}

func (r *changeRecorder) ItemChanged(change out.ItemChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, change)
}

func (r *changeRecorder) ProviderStaleChanged(out.ProviderStaleChange) {}

func (r *changeRecorder) taken() []out.ItemChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.items
	r.items = nil
	return items
}

func storages(t *testing.T) []storage {
	t.Helper()
	database, err := commonsqlite.Open(context.Background(), commonsqlite.Config{Path: filepath.Join(t.TempDir(), "job.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, commonsqlite.Migrate(context.Background(), database, migrations.Files))
	sqliteClock, memoryClock := clock.NewFixed(t0), clock.NewFixed(t0)
	stores := sqlite.NewSQLiteJobStores(database)
	memoryChanges, sqliteChanges := &changeRecorder{}, &changeRecorder{}
	mem := memory.NewMemoryJobStorage(memoryClock).NotifyChangesTo(memoryChanges)
	return []storage{
		{"memory", mem, mem, mem, mem, mem, mem, memoryClock, mem, memoryChanges},
		{"sqlite", sqlite.NewSQLiteRemoteItemRepository(database, sqliteClock, sqliteChanges), stores, stores, stores, stores, stores, sqliteClock, stores, sqliteChanges},
	}
}

func forEach(t *testing.T, test func(t *testing.T, s storage)) {
	t.Helper()
	for _, s := range storages(t) {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			test(t, s)
		})
	}
}

var torbox = must(remoteitem.NewProviderID("torbox"))

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var magnetSeq atomic.Int64

// newSource returns a distinct magnet each call.
func newSource() (remoteitem.Source, remoteitem.Fingerprint, string) {
	hash := fmt.Sprintf("%040x", magnetSeq.Add(1))
	source := remoteitem.NewMagnetSource("magnet:?xt=urn:btih:" + hash + "&dn=item")
	fingerprint, providerHash, err := remoteitem.Identify(source)
	if err != nil {
		panic(err)
	}
	return source, fingerprint, providerHash
}

func newJob(t *testing.T, owner string) remoteitem.Job {
	t.Helper()
	job, err := remoteitem.NewJob(must(remoteitem.NewJobID(id.New())), owner, owner, "", t0)
	require.NoError(t, err)
	return job
}

// start creates a submitting item for owner and returns it.
func start(t *testing.T, s storage, owner string) *remoteitem.RemoteItem {
	t.Helper()
	return startWithClass(t, s, owner, "uncached")
}

// startWithClass creates a submitting item for owner, recording an accepted
// submit of the given rate class.
func startWithClass(t *testing.T, s storage, owner, class string) *remoteitem.RemoteItem {
	t.Helper()
	source, fingerprint, hash := newSource()
	var created *remoteitem.RemoteItem
	err := s.items.JoinOrStart(context.Background(), torbox, fingerprint,
		func(_ context.Context, existing *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
			require.Nil(t, existing)
			item, err := remoteitem.StartSubmission(must(remoteitem.NewID(id.New())), torbox, source, fingerprint, hash, newJob(t, owner), class == "cached", t0)
			created = item
			return out.JoinOrStartResult{Item: item, SubmitClass: class}, err
		})
	require.NoError(t, err)
	return created
}

func observation(t *testing.T, providerItemID string, state remoteitem.State) remoteitem.Observation {
	t.Helper()
	file := must(remoteitem.NewRemoteFile("10", "Sintel/Sintel.mp4", 129241752))
	bundle := remoteitem.NewBundleAvailability(remoteitem.BundleFormatZip, false)
	eta, peers := 90*time.Second, 7
	expires := t0.Add(24 * time.Hour)
	o, err := remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: must(remoteitem.NewProviderItemID(providerItemID)), Kind: remoteitem.KindTorrent, State: state,
		Name: "Sintel", SizeBytes: 129302391, Files: []remoteitem.RemoteFile{file}, Bundle: &bundle, PerFileDelivery: true,
		ExpiresAt: &expires, Telemetry: remoteitem.Telemetry{Progress: 0.5, BytesPerSecond: 1000, ETA: &eta, Peers: &peers},
		ObservedAt: t0,
	})
	require.NoError(t, err)
	return o
}

func update(t *testing.T, s storage, item *remoteitem.RemoteItem, change func(item *remoteitem.RemoteItem) error) {
	t.Helper()
	require.NoError(t, s.items.UpdateRemoteItem(context.Background(), item.ID(),
		func(_ context.Context, loaded *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			return loaded, change(loaded)
		}))
}

func TestRemoteItemRepository_round_trip(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")

		loaded, err := s.items.RemoteItem(ctx, item.ID())
		require.NoError(t, err)
		assert.Equal(t, remoteitem.StateSubmitting, loaded.State())
		assert.Equal(t, item.Source(), loaded.Source())
		submission, ok := loaded.Submission()
		require.True(t, ok)
		assert.Equal(t, 1, submission.Attempts)
		assert.True(t, submission.AttemptStartedAt.Equal(t0))

		update(t, s, item, func(i *remoteitem.RemoteItem) error {
			return i.CompleteSubmission(must(remoteitem.NewProviderItemID("42")), observation(t, "42", remoteitem.StateDownloading), t0)
		})
		loaded, err = s.items.RemoteItem(ctx, item.ID())
		require.NoError(t, err)
		assert.Equal(t, remoteitem.StateDownloading, loaded.State())
		assert.Equal(t, "Sintel", loaded.Name())
		assert.Equal(t, []remoteitem.RemoteFile{must(remoteitem.NewRemoteFile("10", "Sintel/Sintel.mp4", 129241752))}, loaded.Files())
		require.NotNil(t, loaded.Bundle())
		assert.Equal(t, remoteitem.BundleFormatZip, loaded.Bundle().Format())
		require.NotNil(t, loaded.Telemetry().ETA)
		assert.Equal(t, 90*time.Second, *loaded.Telemetry().ETA)
		assert.True(t, loaded.Source().IsZero())
		assert.Empty(t, loaded.Events())
	})
}

func TestRemoteItemRepository_keeps_the_cached_class(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		source, fingerprint, hash := newSource()
		var created *remoteitem.RemoteItem
		require.NoError(t, s.items.JoinOrStart(ctx, torbox, fingerprint,
			func(_ context.Context, _ *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
				item, err := remoteitem.StartSubmission(must(remoteitem.NewID(id.New())), torbox, source, fingerprint, hash, newJob(t, "alice"), true, t0)
				created = item
				return out.JoinOrStartResult{Item: item, SubmitClass: "cached"}, err
			}))

		loaded, err := s.items.RemoteItem(ctx, created.ID())
		require.NoError(t, err)
		submission, ok := loaded.Submission()
		require.True(t, ok)
		assert.True(t, submission.Cached)
	})
}

func TestRemoteItemRepository_join(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		item := start(t, s, "alice")

		err := s.items.JoinOrStart(context.Background(), torbox, item.Fingerprint(),
			func(_ context.Context, existing *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
				require.NotNil(t, existing, "the claimable submitting item is found")
				return out.JoinOrStartResult{Item: existing}, existing.Claim(newJob(t, "bob"), t0)
			})

		require.NoError(t, err)
		loaded, err := s.items.RemoteItem(context.Background(), item.ID())
		require.NoError(t, err)
		assert.Len(t, loaded.Claims(), 2)
	})
}

func TestRemoteItemRepository_update_errors(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")
		failure := errors.New("update function failed")

		err := s.items.UpdateRemoteItem(ctx, item.ID(), func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			_ = i.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureSubmissionLost, ""), t0)
			return nil, failure
		})
		require.ErrorIs(t, err, failure)
		loaded, err := s.items.RemoteItem(ctx, item.ID())
		require.NoError(t, err)
		assert.Equal(t, remoteitem.StateSubmitting, loaded.State(), "rolled back")

		unknown := must(remoteitem.NewID(id.New()))
		_, err = s.items.RemoteItem(ctx, unknown)
		assert.Equal(t, remoteitem.NotFoundError{ID: unknown.String()}, err)
	})
}

func TestRemoteItemRepository_update_by_job_checks_the_actor(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		item := start(t, s, "alice")
		jobID := item.Claims()[0].ID()
		noop := func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			return i, nil
		}

		err := s.items.UpdateRemoteItemByJob(context.Background(), jobID, remoteitem.Actor{UserID: "bob", Role: remoteitem.RoleUser}, noop)
		assert.Equal(t, remoteitem.JobNotFoundError{JobID: jobID.String()}, err)
		assert.NoError(t, s.items.UpdateRemoteItemByJob(context.Background(), jobID, remoteitem.Actor{UserID: "root", Role: remoteitem.RoleAdmin}, noop))
		assert.NoError(t, s.items.UpdateRemoteItemByJob(context.Background(), jobID, remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser}, noop))
	})
}

func TestRemoteItemRepository_release_writes_history_and_outbox(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")
		update(t, s, item, func(i *remoteitem.RemoteItem) error {
			return i.CompleteSubmission(must(remoteitem.NewProviderItemID("42")), observation(t, "42", remoteitem.StateDownloading), t0)
		})
		jobID := item.Claims()[0].ID()

		require.NoError(t, s.items.UpdateRemoteItemByJob(ctx, jobID, remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser},
			func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				return i, i.Release(jobID, remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser}, t0)
			}))

		history, err := s.jobs.JobHistory(ctx, "alice")
		require.NoError(t, err)
		require.Len(t, history, 1)
		assert.Equal(t, "downloading", history[0].FinalState)
		entry, found, err := s.jobs.HistoryEntry(ctx, jobID.String())
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, item.Fingerprint().String(), entry.Fingerprint)

		due, err := s.outbox.DueMessages(ctx, t0, 10)
		require.NoError(t, err)
		require.Len(t, due, 1)
		assert.Equal(t, out.MessageKindDeleteRemoteItem, due[0].Kind)
		assert.Equal(t, item.ID(), due[0].RemoteItemID)
	})
}

func TestRemoteItemRepository_reports_committed_changes_only(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")
		started := s.changes.taken()
		require.Len(t, started, 1)
		assert.Equal(t, out.ItemChange{RemoteItemID: item.ID(), ProviderID: torbox, OwnersAfter: []string{"alice"}}, started[0])

		require.NoError(t, s.items.JoinOrStart(ctx, torbox, item.Fingerprint(),
			func(_ context.Context, existing *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
				return out.JoinOrStartResult{Item: existing}, existing.Claim(newJob(t, "bob"), t0)
			}))
		joined := s.changes.taken()
		require.Len(t, joined, 1)
		assert.Equal(t, []string{"alice"}, joined[0].OwnersBefore)
		assert.ElementsMatch(t, []string{"alice", "bob"}, joined[0].OwnersAfter)

		failure := errors.New("update function failed")
		err := s.items.UpdateRemoteItem(ctx, item.ID(), func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			return nil, failure
		})
		require.ErrorIs(t, err, failure)
		assert.Empty(t, s.changes.taken(), "a rolled-back change is not reported")

		aliceJob := item.Claims()[0].ID()
		alice := remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser}
		require.NoError(t, s.items.UpdateRemoteItemByJob(ctx, aliceJob, alice,
			func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				return i, i.Release(aliceJob, alice, t0)
			}))
		released := s.changes.taken()
		require.Len(t, released, 1)
		assert.ElementsMatch(t, []string{"alice", "bob"}, released[0].OwnersBefore)
		assert.Equal(t, []string{"bob"}, released[0].OwnersAfter)
		assert.Equal(t, []out.ReleasedJob{{JobID: aliceJob, OwnerID: "alice"}}, released[0].ReleasedJobs)
	})
}

// Concurrent submissions of the same content end in one item (DEC-32).
func TestRemoteItemRepository_parallel_starts_of_the_same_content(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		source, fingerprint, hash := newSource()
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				err := s.items.JoinOrStart(context.Background(), torbox, fingerprint,
					func(_ context.Context, existing *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
						job := newJob(t, fmt.Sprintf("user-%d", i))
						if existing != nil {
							return out.JoinOrStartResult{Item: existing}, existing.Claim(job, t0)
						}
						item, err := remoteitem.StartSubmission(must(remoteitem.NewID(id.New())), torbox, source, fingerprint, hash, job, false, t0)
						return out.JoinOrStartResult{Item: item}, err
					})
				assert.NoError(t, err)
			})
		}
		wg.Wait()

		items, err := s.items.RemoteItemsOfProvider(context.Background(), torbox)
		require.NoError(t, err)
		var matching []*remoteitem.RemoteItem
		for _, item := range items {
			if item.Fingerprint() == fingerprint {
				matching = append(matching, item)
			}
		}
		require.Len(t, matching, 1)
		assert.Len(t, matching[0].Claims(), 8)
	})
}

// A user with one free slot racing several starts gets exactly one (ASM-S01).
func TestRemoteItemRepository_parallel_quota_check(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		require.NoError(t, s.quotas.SetSlotQuota(context.Background(), must(slotquota.NewSlotQuota("alice", 1))))
		var succeeded atomic.Int32
		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() {
				source, fingerprint, hash := newSource()
				err := s.items.JoinOrStart(context.Background(), torbox, fingerprint,
					func(ctx context.Context, _ *remoteitem.RemoteItem, reads out.RemoteItemReads) (out.JoinOrStartResult, error) {
						used, err := reads.ActiveChargedItemCount(ctx, "alice")
						if err != nil {
							return out.JoinOrStartResult{}, err
						}
						quota, _, err := reads.SlotQuota(ctx, "alice")
						if err != nil {
							return out.JoinOrStartResult{}, err
						}
						if err := quota.AllowsNewCharge(used); err != nil {
							return out.JoinOrStartResult{}, err
						}
						item, err := remoteitem.StartSubmission(must(remoteitem.NewID(id.New())), torbox, source, fingerprint, hash, newJob(t, "alice"), false, t0)
						return out.JoinOrStartResult{Item: item}, err
					})
				if err == nil {
					succeeded.Add(1)
				}
			})
		}
		wg.Wait()

		assert.Equal(t, int32(1), succeeded.Load())
	})
}

func TestRemoteItemReads(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		gone := start(t, s, "alice")
		update(t, s, gone, func(i *remoteitem.RemoteItem) error {
			return i.CompleteSubmission(must(remoteitem.NewProviderItemID("42")), observation(t, "42", remoteitem.StateDownloading), t0)
		})
		update(t, s, gone, func(i *remoteitem.RemoteItem) error { return i.MarkGone(t0) })
		other := start(t, s, "bob")

		require.NoError(t, s.items.UpdateRemoteItem(ctx, other.ID(), func(ctx context.Context, i *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			holders, err := reads.ItemsHoldingProviderItem(ctx, torbox, must(remoteitem.NewProviderItemID("42")))
			require.NoError(t, err)
			require.Len(t, holders, 1)
			assert.Equal(t, gone.ID(), holders[0].ID)
			assert.Equal(t, remoteitem.StateGone, holders[0].State)

			matching, err := reads.ClaimableItemsMatching(ctx, torbox, other.Fingerprint(), "")
			require.NoError(t, err)
			require.Len(t, matching, 1)
			matching, err = reads.ClaimableItemsMatching(ctx, torbox, gone.Fingerprint(), gone.ProviderHash())
			require.NoError(t, err)
			assert.Empty(t, matching, "gone is not claimable")

			submits, err := reads.SubmitCountSince(ctx, "bob", "uncached", t0.Add(-time.Hour))
			require.NoError(t, err)
			assert.Equal(t, 1, submits)

			oldest, err := reads.OldestSubmitSince(ctx, "bob", "uncached", t0.Add(-time.Hour))
			require.NoError(t, err)
			assert.True(t, oldest.Equal(t0), "oldest submit is the only submit")

			empty, err := reads.OldestSubmitSince(ctx, "nobody", "uncached", t0.Add(-time.Hour))
			require.NoError(t, err)
			assert.True(t, empty.IsZero(), "no submits for an unknown user")

			parked, err := reads.IsDeletionParked(ctx, gone.ID())
			require.NoError(t, err)
			assert.False(t, parked)
			return i, nil
		}))
	})
}

// The quota count must match RemoteItem.IsChargedTo (ADR 0006 compliance).
func TestQuotaUsage_matches_the_domain_predicate(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		first := start(t, s, "alice")
		second := start(t, s, "bob")
		require.NoError(t, s.items.JoinOrStart(ctx, torbox, second.Fingerprint(),
			func(_ context.Context, existing *remoteitem.RemoteItem, _ out.RemoteItemReads) (out.JoinOrStartResult, error) {
				return out.JoinOrStartResult{Item: existing}, existing.Claim(newJob(t, "alice"), t0)
			}))
		bobJob := second.Claims()[0].ID()
		require.NoError(t, s.items.UpdateRemoteItemByJob(ctx, bobJob, remoteitem.Actor{UserID: "bob", Role: remoteitem.RoleUser},
			func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				return i, i.Release(bobJob, remoteitem.Actor{UserID: "bob", Role: remoteitem.RoleUser}, t0)
			}))

		usage, err := s.jobs.QuotaUsage(ctx, "alice", 3)
		require.NoError(t, err)
		charged := 0
		for _, itemID := range []remoteitem.ID{first.ID(), second.ID()} {
			item, err := s.items.RemoteItem(ctx, itemID)
			require.NoError(t, err)
			if item.IsChargedTo("alice") {
				charged++
			}
		}
		assert.Equal(t, 2, charged, "her own item plus the spread charge")
		assert.Equal(t, in.QuotaUsageResult{SlotsUsed: charged, MaxActiveJobs: 3}, usage)

		views, err := s.jobs.JobsOfUser(ctx, "alice")
		require.NoError(t, err)
		require.Len(t, views, 2)
		for _, view := range views {
			assert.True(t, view.SlotCharged)
		}
	})
}

func TestSlotQuotaOf_reports_the_default_and_a_stored_quota(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()

		result, err := s.jobs.SlotQuotaOf(ctx, "alice", 3)
		require.NoError(t, err)
		assert.Equal(t, in.SlotQuotaResult{MaxActiveJobs: 3, IsDefault: true, SlotsUsed: 0}, result)

		require.NoError(t, s.quotas.SetSlotQuota(ctx, must(slotquota.NewSlotQuota("alice", 7))))
		result, err = s.jobs.SlotQuotaOf(ctx, "alice", 3)
		require.NoError(t, err)
		assert.Equal(t, in.SlotQuotaResult{MaxActiveJobs: 7, IsDefault: false, SlotsUsed: 0}, result)

		// an unknown user answers the default with no used slots
		result, err = s.jobs.SlotQuotaOf(ctx, "nobody", 3)
		require.NoError(t, err)
		assert.Equal(t, in.SlotQuotaResult{MaxActiveJobs: 3, IsDefault: true, SlotsUsed: 0}, result)
	})
}

func TestSubmitRateUsage_counts_and_finds_the_oldest_per_class(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		startWithClass(t, s, "alice", "uncached") // accepted_at = t0
		s.clock.Advance(10 * time.Minute)
		startWithClass(t, s, "alice", "uncached") // t0 + 10m
		s.clock.Advance(10 * time.Minute)
		startWithClass(t, s, "alice", "cached") // t0 + 20m

		usage, err := s.jobs.SubmitRateUsage(ctx, "alice", s.clock.Now())
		require.NoError(t, err)

		assert.Equal(t, 2, usage.Uncached.Used)
		assert.True(t, usage.Uncached.Oldest.Equal(t0), "the oldest uncached submit")
		assert.Equal(t, 1, usage.Cached.Used)
		assert.True(t, usage.Cached.Oldest.Equal(t0.Add(20*time.Minute)), "the oldest cached submit")

		// A full hour later every submit has left the rolling window.
		s.clock.Advance(time.Hour)
		usage, err = s.jobs.SubmitRateUsage(ctx, "alice", s.clock.Now())
		require.NoError(t, err)
		assert.Zero(t, usage.Uncached.Used)
		assert.True(t, usage.Uncached.Oldest.IsZero())
		assert.Zero(t, usage.Cached.Used)
		assert.True(t, usage.Cached.Oldest.IsZero())

		// An unknown user has no submits.
		usage, err = s.jobs.SubmitRateUsage(ctx, "nobody", s.clock.Now())
		require.NoError(t, err)
		assert.Zero(t, usage.Uncached.Used)
		assert.Zero(t, usage.Cached.Used)
	})
}

func TestReadModels(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")
		jobID := item.Claims()[0].ID().String()
		details, err := s.jobs.JobDetails(ctx, jobID)
		require.NoError(t, err)
		assert.Equal(t, "submitting", details.State)
		assert.Equal(t, "item", details.DisplayName, "the name from the magnet until the provider reports one")
		assert.False(t, details.Shared)
		assert.True(t, details.SlotCharged)
		_, err = s.jobs.JobDetails(ctx, id.New())
		var notFound remoteitem.JobNotFoundError
		assert.ErrorAs(t, err, &notFound)
		ofItem, err := s.jobs.JobsOfItem(ctx, details.RemoteItemID)
		require.NoError(t, err)
		require.Len(t, ofItem, 1)
		assert.Equal(t, jobID, ofItem[0].ID)
		none, err := s.jobs.JobsOfItem(ctx, id.New())
		require.NoError(t, err)
		assert.Empty(t, none)

		all, err := s.jobs.AllJobs(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 1)
		_, found, err := s.jobs.HistoryEntry(ctx, jobID)
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestOutboxRepository(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		item := start(t, s, "alice")
		update(t, s, item, func(i *remoteitem.RemoteItem) error {
			return i.CompleteSubmission(must(remoteitem.NewProviderItemID("42")), observation(t, "42", remoteitem.StateReady), t0)
		})
		jobID := item.Claims()[0].ID()
		require.NoError(t, s.items.UpdateRemoteItemByJob(ctx, jobID, remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser},
			func(_ context.Context, i *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
				return i, i.Release(jobID, remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser}, t0)
			}))
		due, err := s.outbox.DueMessages(ctx, t0, 10)
		require.NoError(t, err)
		require.Len(t, due, 1)

		require.NoError(t, s.outbox.RecordFailure(ctx, due[0].ID, t0.Add(time.Minute), "timeout", false, t0))
		due, err = s.outbox.DueMessages(ctx, t0, 10)
		require.NoError(t, err)
		assert.Empty(t, due, "not due before the back-off")
		due, err = s.outbox.DueMessages(ctx, t0.Add(time.Minute), 10)
		require.NoError(t, err)
		require.Len(t, due, 1)
		assert.Equal(t, 1, due[0].Attempts)

		require.NoError(t, s.outbox.RecordFailure(ctx, due[0].ID, t0.Add(time.Hour), "gave up", true, t0))
		parked, err := s.jobs.(out.ParkedOutboxMessagesReadModel).ParkedOutboxMessages(ctx)
		require.NoError(t, err)
		require.Len(t, parked, 1)
		assert.Equal(t, "gave up", parked[0].LastError)
		require.NoError(t, s.items.UpdateRemoteItem(ctx, item.ID(), func(ctx context.Context, i *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			isParked, err := reads.IsDeletionParked(ctx, item.ID())
			assert.True(t, isParked)
			return i, err
		}))

		require.NoError(t, s.outbox.MarkProcessed(ctx, due[0].ID, t0))
		due, err = s.outbox.DueMessages(ctx, t0.Add(2*time.Hour), 10)
		require.NoError(t, err)
		assert.Empty(t, due)
	})
}

func TestSyncStateAndUnmanagedItems(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()

		last, err := s.syncs.LastAuthoritativeSync(ctx, torbox)
		require.NoError(t, err)
		assert.True(t, last.IsZero())
		require.NoError(t, s.syncs.RecordAuthoritativeSync(ctx, torbox, t0))
		last, err = s.syncs.LastAuthoritativeSync(ctx, torbox)
		require.NoError(t, err)
		assert.True(t, last.Equal(t0))

		require.NoError(t, s.leftover.RecordUnmanagedItems(ctx, torbox, []out.UnmanagedItem{{ProviderItemID: "9", Kind: "torrent", Name: "x", State: "ready"}}, t0))
		require.NoError(t, s.leftover.RecordUnmanagedItems(ctx, torbox, []out.UnmanagedItem{{ProviderItemID: "9", Kind: "torrent", Name: "x", State: "ready"}}, t0.Add(time.Hour)))
		views, err := s.jobs.(out.UnmanagedItemsReadModel).UnmanagedItems(ctx)
		require.NoError(t, err)
		require.Len(t, views, 1)
		assert.True(t, views[0].FirstSeenAt.Equal(t0))
		assert.True(t, views[0].LastSeenAt.Equal(t0.Add(time.Hour)))
	})
}

func TestRetentionRepository_prunes_only_expired_records(t *testing.T) {
	t.Parallel()
	forEach(t, func(t *testing.T, s storage) {
		ctx := context.Background()
		discarded := start(t, s, "alice")
		update(t, s, discarded, func(i *remoteitem.RemoteItem) error {
			return i.DiscardSubmission(remoteitem.NewFailureReason(remoteitem.FailureProviderRejected, "bad"), t0)
		})
		require.NoError(t, s.leftover.RecordUnmanagedItems(ctx, torbox, []out.UnmanagedItem{{ProviderItemID: "9", Kind: "torrent"}}, t0))
		s.clock.Advance(time.Hour)
		kept := start(t, s, "bob")
		update(t, s, kept, func(i *remoteitem.RemoteItem) error {
			return i.DiscardSubmission(remoteitem.NewFailureReason(remoteitem.FailureProviderRejected, "bad"), s.clock.Now())
		})

		cutoff := t0.Add(time.Minute)
		require.NoError(t, s.prune.PruneExpiredRecords(ctx, out.RetentionCutoffs{
			JobHistoryReleasedBefore: cutoff, UnmanagedItemsSeenBefore: cutoff, ProcessedOutboxBefore: cutoff,
			DeletedItemsBefore: cutoff, SubmitRateWindowsBefore: cutoff,
		}))

		_, err := s.items.RemoteItem(ctx, discarded.ID())
		assert.Equal(t, remoteitem.NotFoundError{ID: discarded.ID().String()}, err)
		_, err = s.items.RemoteItem(ctx, kept.ID())
		assert.NoError(t, err)
		aliceHistory, err := s.jobs.JobHistory(ctx, "alice")
		require.NoError(t, err)
		assert.Empty(t, aliceHistory)
		bobHistory, err := s.jobs.JobHistory(ctx, "bob")
		require.NoError(t, err)
		assert.Len(t, bobHistory, 1)
		unmanaged, err := s.jobs.(out.UnmanagedItemsReadModel).UnmanagedItems(ctx)
		require.NoError(t, err)
		assert.Empty(t, unmanaged)
		update(t, s, kept, func(*remoteitem.RemoteItem) error { return nil })
		require.NoError(t, s.items.UpdateRemoteItem(ctx, kept.ID(), func(ctx context.Context, i *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
			alice, err := reads.SubmitCountSince(ctx, "alice", "uncached", t0.Add(-time.Hour))
			require.NoError(t, err)
			assert.Zero(t, alice, "submit rows older than the cutoff are gone")
			bob, err := reads.SubmitCountSince(ctx, "bob", "uncached", t0.Add(-time.Hour))
			require.NoError(t, err)
			assert.Equal(t, 1, bob)
			return i, nil
		}))
	})
}
