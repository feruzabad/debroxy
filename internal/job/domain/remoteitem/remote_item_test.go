package remoteitem_test

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

var t0 = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

var (
	alice = remoteitem.Actor{UserID: "alice", Role: remoteitem.RoleUser}
	bob   = remoteitem.Actor{UserID: "bob", Role: remoteitem.RoleUser}
	carol = remoteitem.Actor{UserID: "carol", Role: remoteitem.RoleUser}
	admin = remoteitem.Actor{UserID: "root", Role: remoteitem.RoleAdmin}
)

var jobSeq atomic.Int64

func newJob(t *testing.T, owner string) remoteitem.Job {
	t.Helper()
	id, err := remoteitem.NewJobID(fmt.Sprintf("01927f6e-0000-7000-8000-%012d", jobSeq.Add(1)))
	require.NoError(t, err)
	job, err := remoteitem.NewJob(id, owner, owner, "", t0)
	require.NoError(t, err)
	return job
}

func valid(t *testing.T, item *remoteitem.RemoteItem) {
	t.Helper()
	require.NoError(t, remoteitem.CheckInvariants(item))
}

// submitting returns an item in `submitting` created by alice.
func submitting(t *testing.T) (*remoteitem.RemoteItem, remoteitem.Job) {
	t.Helper()
	id, err := remoteitem.NewID("01927f6e-0000-7000-8000-0000000000a1")
	require.NoError(t, err)
	provider, err := remoteitem.NewProviderID("torbox")
	require.NoError(t, err)
	source := remoteitem.NewMagnetSource("magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian+netinst")
	fingerprint, hash, err := remoteitem.Identify(source)
	require.NoError(t, err)
	creator := newJob(t, "alice")
	item, err := remoteitem.StartSubmission(id, provider, source, fingerprint, hash, creator, false, t0)
	require.NoError(t, err)
	valid(t, item)
	return item, creator
}

func observation(t *testing.T, providerItemID string, state remoteitem.State, at time.Time, progress float64) remoteitem.Observation {
	t.Helper()
	pid, err := remoteitem.NewProviderItemID(providerItemID)
	require.NoError(t, err)
	o, err := remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: pid, Kind: remoteitem.KindTorrent, State: state, Name: "debian", SizeBytes: 100,
		Telemetry: remoteitem.Telemetry{Progress: progress}, ObservedAt: at,
	})
	require.NoError(t, err)
	return o
}

func pid(t *testing.T, s string) remoteitem.ProviderItemID {
	t.Helper()
	p, err := remoteitem.NewProviderItemID(s)
	require.NoError(t, err)
	return p
}

// downloading returns an item completed into `downloading` with provider item 42.
func downloading(t *testing.T) (*remoteitem.RemoteItem, remoteitem.Job) {
	t.Helper()
	item, creator := submitting(t)
	require.NoError(t, item.CompleteSubmission(pid(t, "42"), observation(t, "42", remoteitem.StateDownloading, t0, 0.1), t0))
	valid(t, item)
	item.ClearEvents()
	return item, creator
}

func hasEvent[E remoteitem.Event](item *remoteitem.RemoteItem) (E, bool) {
	for _, e := range item.Events() {
		if typed, ok := e.(E); ok {
			return typed, true
		}
	}
	var zero E
	return zero, false
}

func TestStartSubmission(t *testing.T) {
	t.Parallel()
	item, creator := submitting(t)

	assert.Equal(t, remoteitem.StateSubmitting, item.State())
	assert.Equal(t, creator.ID(), item.ChargeHolder())
	assert.Equal(t, "debian netinst", item.Name(), "from the magnet's dn")
	submission, ok := item.Submission()
	require.True(t, ok)
	assert.Equal(t, 1, submission.Attempts)
	assert.Equal(t, t0, submission.AttemptStartedAt, "the first attempt starts with the submission")
	assert.True(t, item.IsChargedTo("alice"))
	assert.False(t, item.StartAttempt(t0.Add(time.Second)), "one attempt at a time")
}

func TestClaim(t *testing.T) {
	t.Parallel()
	item, creator := submitting(t)

	joined := newJob(t, "bob")
	require.NoError(t, item.Claim(joined, t0))
	valid(t, item)
	assert.Equal(t, remoteitem.OriginJoined, item.Claims()[1].Origin())
	assert.False(t, item.IsChargedTo("bob"), "joining is free while the charge holder exists")
	assert.False(t, item.NewClaimIsCharged())

	assert.ErrorIs(t, item.Claim(joined, t0), remoteitem.ErrDuplicateJobID)
	assert.Equal(t, remoteitem.JobAlreadyExistsError{ExistingJobID: creator.ID()}, item.Claim(newJob(t, "alice"), t0))
}

func TestClaim_refused_outside_claimable_states(t *testing.T) {
	t.Parallel()
	item, _ := submitting(t)
	require.NoError(t, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureProviderRejected, "x"), t0))

	assert.ErrorIs(t, item.Claim(newJob(t, "bob"), t0), remoteitem.ErrRemoteItemUnavailable)
}

func TestRelease_charge_holder_spreads_the_charge(t *testing.T) {
	t.Parallel()
	item, creator := downloading(t)
	require.NoError(t, item.Claim(newJob(t, "bob"), t0))
	require.NoError(t, item.Claim(newJob(t, "carol"), t0))

	require.NoError(t, item.Release(creator.ID(), alice, t0))
	valid(t, item)

	assert.True(t, item.ChargeHolderReleased())
	assert.True(t, item.IsChargedTo("bob"))
	assert.True(t, item.IsChargedTo("carol"))
	assert.False(t, item.IsChargedTo("alice"))
	assert.True(t, item.NewClaimIsCharged(), "later joiners pay too (ADR 0006 rule 5)")
	spread, ok := hasEvent[remoteitem.ChargeSpreadToClaimants](item)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"bob", "carol"}, spread.UserIDs)
	released, ok := hasEvent[remoteitem.JobReleased](item)
	require.True(t, ok)
	assert.Equal(t, remoteitem.StateDownloading, released.FinalState)
}

func TestRelease_only_owner_or_admin(t *testing.T) {
	t.Parallel()
	item, creator := downloading(t)
	joined := newJob(t, "bob")
	require.NoError(t, item.Claim(joined, t0))

	assert.Equal(t, remoteitem.JobNotFoundError{JobID: creator.ID().String()}, item.Release(creator.ID(), bob, t0))
	assert.NoError(t, item.Release(joined.ID(), admin, t0))
	valid(t, item)
}

func TestRelease_last_claim_requests_deletion(t *testing.T) {
	t.Parallel()

	t.Run("active_item_with_provider_item", func(t *testing.T) {
		t.Parallel()
		item, creator := downloading(t)

		require.NoError(t, item.Release(creator.ID(), alice, t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleting, item.State())
		deletion, ok := hasEvent[remoteitem.DeletionRequested](item)
		require.True(t, ok)
		assert.Equal(t, pid(t, "42"), deletion.ProviderItemID)
		_, ok = hasEvent[remoteitem.LastClaimReleased](item)
		assert.True(t, ok)
		released, _ := hasEvent[remoteitem.JobReleased](item)
		assert.Equal(t, remoteitem.StateDownloading, released.FinalState, "never deleting or deleted")
	})

	t.Run("submitting_keeps_the_submission", func(t *testing.T) {
		t.Parallel()
		item, creator := submitting(t)

		require.NoError(t, item.Release(creator.ID(), alice, t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleting, item.State())
		_, hasSubmission := item.Submission()
		assert.True(t, hasSubmission, "the create may still be in flight")
		_, ok := hasEvent[remoteitem.DeletionRequested](item)
		assert.False(t, ok, "no provider item to delete yet")
	})

	t.Run("gone_goes_straight_to_deleted", func(t *testing.T) {
		t.Parallel()
		item, creator := downloading(t)
		require.NoError(t, item.MarkGone(t0))

		require.NoError(t, item.Release(creator.ID(), alice, t0.Add(time.Minute)))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleted, item.State())
		assert.Equal(t, t0.Add(time.Minute), item.DeletedAt())
	})

	t.Run("failed_without_provider_item_goes_to_deleted", func(t *testing.T) {
		t.Parallel()
		item, _ := submitting(t)
		joined := newJob(t, "bob")
		require.NoError(t, item.Claim(joined, t0))
		require.NoError(t, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureSubmissionLost, ""), t0))
		creator := item.Claims()[0]

		require.NoError(t, item.Release(creator.ID(), alice, t0))
		require.NoError(t, item.Release(joined.ID(), bob, t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleted, item.State())
	})
}

func TestCompleteSubmission(t *testing.T) {
	t.Parallel()

	t.Run("takes_the_observed_state", func(t *testing.T) {
		t.Parallel()
		item, _ := submitting(t)

		require.NoError(t, item.CompleteSubmission(pid(t, "42"), observation(t, "42", remoteitem.StateReady, t0, 1), t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateReady, item.State())
		assert.Equal(t, pid(t, "42"), item.ProviderItemID())
		assert.True(t, item.Source().IsZero())
		assert.True(t, item.ChargeHolder().IsZero(), "ready ends the Active Run")
	})

	t.Run("unknown_state_counts_as_queued", func(t *testing.T) {
		t.Parallel()
		item, _ := submitting(t)

		require.NoError(t, item.CompleteSubmission(pid(t, "42"), observation(t, "42", remoteitem.State{}, t0, 0), t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateQueued, item.State())
	})

	t.Run("released_while_submitting_stays_deleting", func(t *testing.T) {
		t.Parallel()
		item, creator := submitting(t)
		require.NoError(t, item.Release(creator.ID(), alice, t0))
		item.ClearEvents()

		require.NoError(t, item.CompleteSubmission(pid(t, "42"), observation(t, "42", remoteitem.StateReady, t0, 1), t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleting, item.State())
		_, ok := hasEvent[remoteitem.DeletionRequested](item)
		assert.True(t, ok)
	})
}

func TestFailures(t *testing.T) {
	t.Parallel()
	reason := remoteitem.NewFailureReason(remoteitem.FailureProviderRejected, "bad magnet")

	t.Run("discard_with_only_the_creator", func(t *testing.T) {
		t.Parallel()
		item, creator := submitting(t)

		require.NoError(t, item.DiscardSubmission(reason, t0))
		valid(t, item)

		assert.Equal(t, remoteitem.StateDeleted, item.State())
		released, ok := hasEvent[remoteitem.JobReleased](item)
		require.True(t, ok, "C7-04")
		assert.Equal(t, creator.ID(), released.Job.ID())
		assert.Equal(t, remoteitem.StateSubmitting, released.FinalState)
		assert.Equal(t, reason, released.FailureReason)
		_, ok = hasEvent[remoteitem.SubmissionFailed](item)
		assert.True(t, ok)
		_, ok = hasEvent[remoteitem.LifecycleAdvanced](item)
		assert.True(t, ok)
	})

	t.Run("discard_refused_with_joiners", func(t *testing.T) {
		t.Parallel()
		item, _ := submitting(t)
		require.NoError(t, item.Claim(newJob(t, "bob"), t0))

		assert.Error(t, item.DiscardSubmission(reason, t0))
		require.NoError(t, item.FailSubmission(reason, t0))
		valid(t, item)
		assert.Equal(t, remoteitem.StateFailed, item.State())
		assert.Equal(t, reason, item.FailureReason())
	})

	t.Run("fail_while_released_goes_to_deleted", func(t *testing.T) {
		t.Parallel()
		item, creator := submitting(t)
		require.NoError(t, item.Release(creator.ID(), alice, t0))

		require.NoError(t, item.FailSubmission(reason, t0))
		valid(t, item)
		assert.Equal(t, remoteitem.StateDeleted, item.State())
	})
}

func TestAttempts(t *testing.T) {
	t.Parallel()
	item, _ := submitting(t)

	require.NoError(t, item.BlockAttempt(t0, time.Hour))
	submission, _ := item.Submission()
	assert.Equal(t, 0, submission.Attempts, "a create that was never sent does not count")
	assert.True(t, submission.AttemptStartedAt.IsZero())
	assert.False(t, item.StartAttempt(t0.Add(30*time.Minute)), "NotBefore has not passed")
	assert.True(t, item.StartAttempt(t0.Add(time.Hour)))

	require.NoError(t, item.RecordAmbiguousFailure(t0.Add(time.Hour), []time.Duration{30 * time.Second, 2 * time.Minute}))
	assert.Equal(t, 1, item.TakeDueFollowUps(t0.Add(time.Hour+time.Minute)))
	assert.Equal(t, 0, item.TakeDueFollowUps(t0.Add(time.Hour+time.Minute)))
	assert.Equal(t, 1, item.TakeDueFollowUps(t0.Add(2*time.Hour)))

	require.NoError(t, item.EndAttempt(), "C7-02: late deletion check")
	submission, _ = item.Submission()
	assert.True(t, submission.AttemptStartedAt.IsZero())
	valid(t, item)
}

func TestDeferSubmission(t *testing.T) {
	t.Parallel()
	item, _ := submitting(t)
	blocker, err := remoteitem.NewID("01927f6e-0000-7000-8000-0000000000b2")
	require.NoError(t, err)

	require.NoError(t, item.DeferSubmission(blocker, t0))
	submission, _ := item.Submission()
	assert.Equal(t, blocker, submission.DeferredBehind)
	assert.True(t, submission.AttemptStartedAt.IsZero(), "C7-02")

	assert.True(t, item.StartAttempt(t0.Add(time.Minute)), "resumed after the blocker's deletion")
	submission, _ = item.Submission()
	assert.True(t, submission.DeferredBehind.IsZero())
	valid(t, item)
}

func TestObserve(t *testing.T) {
	t.Parallel()

	t.Run("allowed_move", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)

		require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateReady, t0.Add(time.Minute), 1)))
		valid(t, item)
		assert.Equal(t, remoteitem.StateReady, item.State())
		assert.True(t, item.ChargeHolder().IsZero())
	})

	t.Run("forbidden_move_is_an_anomaly", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)
		require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateReady, t0.Add(time.Minute), 1)))
		item.ClearEvents()

		require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateDownloading, t0.Add(2*time.Minute), 0.5)))
		assert.Equal(t, remoteitem.StateReady, item.State())
		_, ok := hasEvent[remoteitem.SyncAnomalyDetected](item)
		assert.True(t, ok)
	})

	t.Run("older_observation_is_ignored", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)
		require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateProcessing, t0.Add(time.Minute), 1)))

		require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateReady, t0.Add(30*time.Second), 1)))
		assert.Equal(t, remoteitem.StateProcessing, item.State())
	})

	t.Run("unknown_state_keeps_state_but_updates_progress", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)

		require.NoError(t, item.Observe(observation(t, "42", remoteitem.State{}, t0.Add(time.Minute), 0.7)))
		assert.Equal(t, remoteitem.StateDownloading, item.State())
		assert.InDelta(t, 0.7, item.Telemetry().Progress, 0.001)
	})
}

func TestStalls(t *testing.T) {
	t.Parallel()
	item, _ := downloading(t)

	assert.False(t, item.DetectStall(t0.Add(29*time.Minute), 30*time.Minute))
	assert.True(t, item.DetectStall(t0.Add(30*time.Minute), 30*time.Minute))
	assert.False(t, item.DetectStall(t0.Add(40*time.Minute), 30*time.Minute), "once per run")
	assert.Equal(t, remoteitem.StallSourceDebroxy, item.Telemetry().StallSource)

	require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateDownloading, t0.Add(41*time.Minute), 0.1)))
	assert.True(t, item.Telemetry().Stalled, "no progress: the stall stays")
	require.NoError(t, item.Observe(observation(t, "42", remoteitem.StateDownloading, t0.Add(42*time.Minute), 0.2)))
	assert.False(t, item.Telemetry().Stalled, "progress clears a debroxy stall")
}

func TestRevive(t *testing.T) {
	t.Parallel()

	t.Run("active_result_charges_every_claimant", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)
		require.NoError(t, item.Claim(newJob(t, "bob"), t0))
		require.NoError(t, item.MarkGone(t0))

		require.NoError(t, item.Revive(observation(t, "42", remoteitem.StateDownloading, t0.Add(time.Minute), 0.5), t0.Add(time.Minute)))
		valid(t, item)
		assert.Equal(t, remoteitem.StateDownloading, item.State())
		assert.True(t, item.IsChargedTo("alice"))
		assert.True(t, item.IsChargedTo("bob"))
	})

	t.Run("ready_result_charges_nobody", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)
		require.NoError(t, item.MarkGone(t0))

		require.NoError(t, item.Revive(observation(t, "42", remoteitem.StateReady, t0.Add(time.Minute), 1), t0.Add(time.Minute)))
		valid(t, item)
		assert.False(t, item.IsChargedTo("alice"))
	})

	t.Run("only_failed_or_gone", func(t *testing.T) {
		t.Parallel()
		item, _ := downloading(t)

		assert.Error(t, item.Revive(observation(t, "42", remoteitem.StateReady, t0, 1), t0))
	})
}

func TestDeletionEnds(t *testing.T) {
	t.Parallel()

	for name, finish := range map[string]func(*remoteitem.RemoteItem, time.Time) error{
		"confirmed":  (*remoteitem.RemoteItem).ConfirmDeleted,
		"superseded": (*remoteitem.RemoteItem).SupersedeDeletion,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item, creator := downloading(t)
			require.NoError(t, item.Release(creator.ID(), alice, t0))

			require.NoError(t, finish(item, t0.Add(time.Minute)))
			valid(t, item)
			assert.Equal(t, remoteitem.StateDeleted, item.State())
			assert.Equal(t, t0.Add(time.Minute), item.DeletedAt())
			assert.Error(t, finish(item, t0.Add(2*time.Minute)))
		})
	}
}

func TestRename_only_owner(t *testing.T) {
	t.Parallel()
	item, creator := downloading(t)

	require.NoError(t, item.Rename(creator.ID(), alice, "Debian"))
	assert.Equal(t, "Debian", item.Claims()[0].DisplayName())
	assert.Error(t, item.Rename(creator.ID(), admin, "x"), "rename is for owners only")
	assert.Error(t, item.Rename(creator.ID(), carol, "x"))
}

func TestRename_trims_and_limits_the_display_name(t *testing.T) {
	t.Parallel()
	item, creator := downloading(t)

	require.NoError(t, item.Rename(creator.ID(), alice, "  Sintel  "))
	assert.Equal(t, "Sintel", item.Claims()[0].DisplayName(), "the name is trimmed")

	assert.Equal(t, remoteitem.InvalidDisplayNameError{}, item.Rename(creator.ID(), alice, "   "), "empty after trim")
	assert.Equal(t, remoteitem.InvalidDisplayNameError{Length: 256}, item.Rename(creator.ID(), alice, strings.Repeat("é", 256)))
}

func TestNewJob_trims_and_limits_the_display_name(t *testing.T) {
	t.Parallel()
	id, err := remoteitem.NewJobID("01927f6e-0000-7000-8000-000000000099")
	require.NoError(t, err)

	job, err := remoteitem.NewJob(id, "alice", "alice", "  Sintel  ", t0)
	require.NoError(t, err)
	assert.Equal(t, "Sintel", job.DisplayName())

	_, err = remoteitem.NewJob(id, "alice", "alice", strings.Repeat("é", 256), t0)
	assert.Equal(t, remoteitem.InvalidDisplayNameError{Length: 256}, err)
}

func TestHandOverClaim_moves_without_history_and_finishes_the_last(t *testing.T) {
	t.Parallel()
	item, creator := submitting(t)
	joiner := newJob(t, "bob")
	require.NoError(t, item.Claim(joiner, t0))
	require.NoError(t, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureMergedInto, "other"), t0))
	item.ClearEvents()

	moved, err := item.HandOverClaim(joiner.ID(), t0)
	require.NoError(t, err)
	assert.Equal(t, joiner.ID(), moved.ID())
	_, released := hasEvent[remoteitem.JobReleased](item)
	assert.False(t, released, "a moved job writes no history")
	assert.Equal(t, remoteitem.StateFailed, item.State())

	_, err = item.HandOverClaim(creator.ID(), t0)
	require.NoError(t, err)
	assert.Equal(t, remoteitem.StateDeleted, item.State(), "failed without provider item and no claims")
	valid(t, item)

	_, err = item.HandOverClaim(creator.ID(), t0)
	assert.Equal(t, remoteitem.JobNotFoundError{JobID: creator.ID().String()}, err)

	item.RecordRefusedHandOver(creator, remoteitem.NewFailureReason(remoteitem.FailureMergedRefused, "job-already-exists"))
	event, ok := hasEvent[remoteitem.JobReleased](item)
	require.True(t, ok)
	assert.Equal(t, remoteitem.FailureMergedRefused, event.FailureReason.Code())
}
