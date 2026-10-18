package command_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/job/app/command"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// observed is a listed observation at the fixture's current time.
func (f *fixture) observed(providerItemID, hash string, state remoteitem.State) remoteitem.Observation {
	f.t.Helper()
	pid, err := remoteitem.NewProviderItemID(providerItemID)
	require.NoError(f.t, err)
	observation, err := remoteitem.NewObservation(remoteitem.ObservationParams{
		ProviderItemID: pid, Kind: remoteitem.KindTorrent, ProviderHash: hash, State: state, Name: "listed " + providerItemID, ObservedAt: f.clock.Now(),
	})
	require.NoError(f.t, err)
	return observation
}

func (f *fixture) list(complete bool, observations ...remoteitem.Observation) {
	f.gateway.setListing(out.RemoteItemListing{Observations: observations, Complete: complete}, nil)
}

func (f *fixture) refreshAs(cause string) error {
	return f.refresh.Handle(context.Background(), in.RefreshProviderState{Actor: alice, ProviderID: provider, Cause: cause})
}

func (f *fixture) mustRefresh(cause string) {
	f.t.Helper()
	require.NoError(f.t, f.refreshAs(cause))
}

// --- pipeline ---------------------------------------------------------------

func TestRefresh_observes_known_items(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("sintel"))
	f.clock.Advance(time.Second)
	f.list(true, f.observed("1001", btih("sintel"), remoteitem.StateReady))

	f.mustRefresh(in.RefreshCauseSafety)

	assert.Equal(t, "ready", f.job(jobID).State)
	last, err := f.storage.LastAuthoritativeSync(context.Background(), mustProvider(t))
	require.NoError(t, err)
	assert.Equal(t, f.clock.Now(), last)
}

func mustProvider(t *testing.T) remoteitem.ProviderID {
	t.Helper()
	id, err := remoteitem.NewProviderID(provider)
	require.NoError(t, err)
	return id
}

func TestRefresh_completes_an_interrupted_submission_by_provider_hash(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.AmbiguousCreateError{Cause: "timeout"})
	jobID := f.mustSubmit(alice, magnet("lost-answer"))
	f.list(true, f.observed("1001", btih("lost-answer"), remoteitem.StateDownloading))

	f.mustRefresh(in.RefreshCauseFollowUp)

	view := f.job(jobID)
	assert.Equal(t, "downloading", view.State)
	assert.Equal(t, "1001", view.ProviderItemID)
	_, pending := f.item(jobID).Submission()
	assert.False(t, pending, "follow-ups end with the submission")
}

func TestRefresh_records_the_provider_item_of_a_submission_released_meanwhile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.AmbiguousCreateError{Cause: "timeout"})
	jobID := f.mustSubmit(alice, magnet("released"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))
	f.list(true, f.observed("1001", btih("released"), remoteitem.StateDownloading))

	f.mustRefresh(in.RefreshCauseSafety)

	item := f.itemByID(itemID)
	assert.Equal(t, remoteitem.StateDeleting, item.State())
	assert.Equal(t, "1001", item.ProviderItemID().String())
	messages, err := f.storage.DueMessages(context.Background(), f.clock.Now(), 10)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
}

func TestRefresh_marks_absent_items_gone_after_the_grace_and_revives_them(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("flaky"))
	f.list(true)

	f.mustRefresh(in.RefreshCauseSafety)
	assert.Equal(t, "downloading", f.job(jobID).State, "within the absence grace")

	f.clock.Advance(2 * time.Minute)
	f.mustRefresh(in.RefreshCauseSafety)
	assert.Equal(t, "gone", f.job(jobID).State)

	f.clock.Advance(time.Minute)
	f.list(true, f.observed("1001", btih("flaky"), remoteitem.StateReady))
	f.mustRefresh(in.RefreshCauseSafety)
	view := f.job(jobID)
	assert.Equal(t, "ready", view.State, "revived")
	assert.Empty(t, view.FailureReason)
}

func TestRefresh_revive_is_blocked_while_a_deleting_item_holds_the_provider_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gone := f.mustSubmit(alice, magnet("x"))
	f.clock.Advance(2 * time.Minute)
	f.list(true)
	f.mustRefresh(in.RefreshCauseSafety)
	require.Equal(t, "gone", f.job(gone).State)

	// Another item takes the provider item 1001 and is released: deleting.
	f.mustSubmit(bob, magnet("x-variant"))
	holder := f.mustSubmit(carol, magnet("x-variant"))
	bobs, err := f.storage.JobsOfUser(context.Background(), "bob")
	require.NoError(t, err)
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: bob, JobID: bobs[0].ID}))
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: carol, JobID: holder}))

	f.clock.Advance(time.Minute)
	f.list(true, f.observed("1001", btih("x"), remoteitem.StateReady))
	f.mustRefresh(in.RefreshCauseSafety)

	assert.Equal(t, "gone", f.job(gone).State, "C6-01")
	unmanaged, err := f.storage.UnmanagedItems(context.Background())
	require.NoError(t, err)
	assert.Empty(t, unmanaged, "the observation is dropped, not unmanaged")
}

func TestRefresh_revive_needs_an_observation_later_than_the_last_deletion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gone := f.mustSubmit(alice, magnet("x"))
	f.clock.Advance(2 * time.Minute)
	f.list(true)
	f.mustRefresh(in.RefreshCauseSafety)

	// Another item held 1001 and was confirmed deleted just now.
	other := f.mustSubmit(bob, magnet("x-variant"))
	otherItem := f.job(other).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: bob, JobID: other}))
	observedBefore := f.observed("1001", btih("x"), remoteitem.StateReady)
	f.update(otherItem, func(item *remoteitem.RemoteItem) error { return item.ConfirmDeleted(f.clock.Now()) })

	f.list(true, observedBefore)
	f.mustRefresh(in.RefreshCauseSafety)
	assert.Equal(t, "gone", f.job(gone).State, "C7-01: not later than the deletion")

	f.clock.Advance(time.Second)
	f.list(true, f.observed("1001", btih("x"), remoteitem.StateReady))
	f.mustRefresh(in.RefreshCauseSafety)
	assert.Equal(t, "ready", f.job(gone).State)
}

func TestRefresh_records_leftovers_as_unmanaged_items(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true, f.observed("9999", "feed", remoteitem.StateReady))

	f.mustRefresh(in.RefreshCauseSafety)

	unmanaged, err := f.storage.UnmanagedItems(context.Background())
	require.NoError(t, err)
	require.Len(t, unmanaged, 1)
	assert.Equal(t, "9999", unmanaged[0].ProviderItemID)
	assert.Equal(t, "ready", unmanaged[0].State)
}

func TestRefresh_incomplete_listing_skips_absence_and_starts_the_back_off(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("partial"))
	f.clock.Advance(2 * time.Minute)
	f.list(false)

	assert.Equal(t, remoteitem.ErrStateStale, f.refreshAs(in.RefreshCauseRead))
	assert.Equal(t, "downloading", f.job(jobID).State, "no absence on an incomplete listing")
	last, err := f.storage.LastAuthoritativeSync(context.Background(), mustProvider(t))
	require.NoError(t, err)
	assert.True(t, last.IsZero())

	lists := f.gateway.listCount()
	assert.Equal(t, remoteitem.ErrStateStale, f.refreshAs(in.RefreshCauseRead), "back-off: stored state")
	assert.Equal(t, lists, f.gateway.listCount(), "no provider call during the back-off")

	f.list(true, f.observed("1001", btih("partial"), remoteitem.StateReady))
	f.mustRefresh(in.RefreshCauseSafety) // keeps its own pacing and ends the back-off
	f.mustRefresh(in.RefreshCauseRead)
	assert.Equal(t, "ready", f.job(jobID).State)
}

func TestRefresh_back_off_doubles_up_to_the_maximum(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.setListing(out.RemoteItemListing{}, remoteitem.ProviderUnavailableError{Provider: provider})

	for _, wait := range []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second} {
		require.Error(t, f.refreshAs(in.RefreshCauseSafety))
		f.clock.Advance(wait - time.Millisecond)
		lists := f.gateway.listCount()
		assert.Equal(t, remoteitem.ErrStateStale, f.refreshAs(in.RefreshCauseRead))
		assert.Equal(t, lists, f.gateway.listCount())
		f.clock.Advance(time.Millisecond)
	}
}

func TestRefresh_reports_when_the_back_off_starts_and_ends(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.setListing(out.RemoteItemListing{}, remoteitem.ProviderUnavailableError{Provider: provider})

	require.Error(t, f.refreshAs(in.RefreshCauseSafety))
	assert.Equal(t, []out.ProviderStaleChange{{ProviderID: mustProvider(t), Stale: true}}, f.changes.taken())
	f.clock.Advance(time.Minute)
	require.Error(t, f.refreshAs(in.RefreshCauseSafety))
	assert.Empty(t, f.changes.taken(), "a longer back-off is no new transition")

	f.list(true)
	f.mustRefresh(in.RefreshCauseSafety)
	assert.Equal(t, []out.ProviderStaleChange{{ProviderID: mustProvider(t), Stale: false}}, f.changes.taken())
	f.clock.Advance(time.Minute)
	f.mustRefresh(in.RefreshCauseSafety)
	assert.Empty(t, f.changes.taken())
}

func TestRefresh_read_skips_a_recent_complete_listing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true)

	f.mustRefresh(in.RefreshCauseRead)
	f.clock.Advance(9 * time.Second)
	f.mustRefresh(in.RefreshCauseRead)
	assert.Equal(t, 1, f.gateway.listCount())

	f.clock.Advance(time.Second)
	f.mustRefresh(in.RefreshCauseRead)
	assert.Equal(t, 2, f.gateway.listCount())
}

func TestRefresh_forced_refreshes_are_limited_per_user(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true)

	f.mustRefresh(in.RefreshCauseForced)
	f.mustRefresh(in.RefreshCauseForced)
	assert.Equal(t, command.RefreshRateExceededError{PerMinute: 2, RetryAfterDuration: time.Minute}, f.refreshAs(in.RefreshCauseForced))
	assert.NoError(t, f.refresh.Handle(context.Background(), in.RefreshProviderState{Actor: bob, ProviderID: provider, Cause: in.RefreshCauseForced}))

	f.clock.Advance(time.Minute)
	f.mustRefresh(in.RefreshCauseForced)
}

// blockingGateway lists only after release is closed.
type blockingGateway struct {
	*fakeGateway
	release chan struct{}
}

func (g blockingGateway) ListRemoteItems(ctx context.Context, provider remoteitem.ProviderID) (out.RemoteItemListing, error) {
	<-g.release
	return g.fakeGateway.ListRemoteItems(ctx, provider)
}

func TestRefresh_concurrent_refreshes_share_one_run(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true)
	release := make(chan struct{})
	refresher := command.NewRefresher(command.RefreshPipelineDependencies{
		Submission: command.SubmissionDependencies{Items: f.storage, Provider: blockingGateway{f.gateway, release}, Clock: f.clock, Config: f.config},
		Unmanaged:  f.storage, SyncState: f.storage, AbsenceGrace: func(remoteitem.ProviderID) time.Duration { return 0 },
	}, f.refreshCfg, f.background, logger)
	handler := command.NewRefreshProviderStateHandler(refresher, logger, metrics)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			assert.NoError(t, handler.Handle(context.Background(), in.RefreshProviderState{ProviderID: provider, Cause: in.RefreshCauseSafety}))
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Equal(t, 1, f.gateway.listCount())
}

func TestHandleProviderWebhook_coalesces_bursts_into_one_trailing_refresh(t *testing.T) {
	t.Parallel()
	f := newFixtureWith(t, func(c *command.RefreshConfig) { c.WebhookWindow = 100 * time.Millisecond })
	f.list(true)
	webhook := in.HandleProviderWebhook{ProviderID: provider, PathSecret: "s3cret", Body: []byte(`{"type":"x"}`)}

	for range 5 {
		require.NoError(t, f.webhook.Handle(context.Background(), webhook))
	}
	assert.Eventually(t, func() bool { return f.gateway.listCount() == 1 }, time.Second, 5*time.Millisecond, "immediate refresh")
	assert.Eventually(t, func() bool { return f.gateway.listCount() == 2 }, time.Second, 5*time.Millisecond, "one trailing refresh")
	time.Sleep(250 * time.Millisecond)
	assert.Equal(t, 2, f.gateway.listCount(), "no further refresh without webhooks")

	rejected := webhook
	rejected.PathSecret = "wrong"
	assert.Equal(t, remoteitem.ErrWebhookRejected, f.webhook.Handle(context.Background(), rejected))
}

// --- periodic commands ------------------------------------------------------

func TestExpireSubmissions_fails_attempts_older_than_the_timeout(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.AmbiguousCreateError{Cause: "timeout"})
	ambiguous := f.mustSubmit(alice, magnet("ambiguous"))
	f.gateway.setCreate(failing(remoteitem.CreateNotSentError{RetryAfter: time.Hour}))
	blocked := f.mustSubmit(bob, magnet("blocked"))

	f.clock.Advance(20 * time.Minute)
	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))
	assert.Equal(t, "submitting", f.job(ambiguous).State, "not older than the timeout yet")

	f.clock.Advance(time.Second)
	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))
	assert.Equal(t, "failed", f.job(ambiguous).State)
	assert.Equal(t, remoteitem.FailureSubmissionLost, f.job(ambiguous).FailureReason)
	assert.Equal(t, "submitting", f.job(blocked).State, "a budget wait is no attempt (C6-02)")
}

func TestExpireSubmissions_fails_deferred_items_when_the_blocker_is_parked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustSubmit(alice, magnet("x"))
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))
	deferred := f.mustSubmit(bob, magnet("x-variant"))

	f.clock.Advance(time.Hour)
	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))
	assert.Equal(t, "submitting", f.job(deferred).State, "deferred items have no timeout")

	messages, err := f.storage.DueMessages(context.Background(), f.clock.Now(), 10)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.NoError(t, f.storage.RecordFailure(context.Background(), messages[0].ID, f.clock.Now(), "boom", true, f.clock.Now()))
	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))

	assert.Equal(t, "failed", f.job(deferred).State)
	assert.Equal(t, remoteitem.FailureBlockedByDelete, f.job(deferred).FailureReason)
}

func TestExpireSubmissions_ends_released_submissions_without_a_create_in_flight(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.CreateNotSentError{RetryAfter: time.Hour})
	jobID := f.mustSubmit(alice, magnet("waiting"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))

	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))

	assert.Equal(t, remoteitem.StateDeleted, f.itemByID(itemID).State(), "C7-03")
}

func TestResumeDeferredSubmissions_resumes_after_the_blocker_is_deleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustSubmit(alice, magnet("x"))
	blocker := f.job(old).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))
	deferred := f.mustSubmit(bob, magnet("x-variant"))

	require.NoError(t, f.resume.Handle(context.Background(), in.ResumeDeferredSubmissions{}))
	assert.EqualValues(t, 2, f.gateway.creates.Load(), "the blocker is still deleting")

	f.update(blocker, func(item *remoteitem.RemoteItem) error { return item.ConfirmDeleted(f.clock.Now()) })
	f.clock.Advance(time.Second)
	f.gateway.setCreate(created(t, f.clock, "2002", remoteitem.StateQueued))
	require.NoError(t, f.resume.Handle(context.Background(), in.ResumeDeferredSubmissions{}))

	assert.EqualValues(t, 3, f.gateway.creates.Load())
	assert.Equal(t, "2002", f.job(deferred).ProviderItemID)
}

func TestResumeDeferredSubmissions_reruns_budget_blocked_creates_after_not_before(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.CreateNotSentError{RetryAfter: 10 * time.Minute})
	jobID := f.mustSubmit(alice, magnet("budget"))

	f.clock.Advance(9 * time.Minute)
	require.NoError(t, f.resume.Handle(context.Background(), in.ResumeDeferredSubmissions{}))
	assert.EqualValues(t, 1, f.gateway.creates.Load())

	f.clock.Advance(time.Minute)
	f.gateway.setCreate(created(t, f.clock, "1001", remoteitem.StateQueued))
	require.NoError(t, f.resume.Handle(context.Background(), in.ResumeDeferredSubmissions{}))
	assert.Equal(t, "queued", f.job(jobID).State)
}

func TestDetectStalls_flags_items_without_progress(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("slow"))

	f.clock.Advance(29 * time.Minute)
	require.NoError(t, f.stalls.Handle(context.Background(), in.DetectStalls{}))
	assert.False(t, f.job(jobID).Telemetry.Stalled)

	f.clock.Advance(time.Minute)
	require.NoError(t, f.stalls.Handle(context.Background(), in.DetectStalls{}))
	assert.True(t, f.job(jobID).Telemetry.Stalled)
}

func TestRunFollowUpRefreshes_refreshes_when_due_unless_a_listing_ran_since(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.AmbiguousCreateError{Cause: "timeout"})
	f.mustSubmit(alice, magnet("follow"))
	f.list(true)

	f.clock.Advance(29 * time.Second)
	require.NoError(t, f.followUps.Handle(context.Background(), in.RunFollowUpRefreshes{}))
	assert.Zero(t, f.gateway.listCount(), "not due yet")

	f.clock.Advance(time.Second)
	require.NoError(t, f.followUps.Handle(context.Background(), in.RunFollowUpRefreshes{}))
	assert.Equal(t, 1, f.gateway.listCount())

	f.clock.Advance(91 * time.Second) // the 2 min follow-up is due…
	f.mustRefresh(in.RefreshCauseSafety)
	f.clock.Advance(time.Second) // …but a complete listing ran after it was due
	require.NoError(t, f.followUps.Handle(context.Background(), in.RunFollowUpRefreshes{}))
	assert.Equal(t, 2, f.gateway.listCount())
}

func TestRunSafetyRefreshes_uses_the_short_interval_while_items_are_active(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true)

	require.NoError(t, f.safety.Handle(context.Background(), in.RunSafetyRefreshes{}))
	assert.Equal(t, 1, f.gateway.listCount(), "first tick after start")
	f.clock.Advance(5 * time.Minute)
	require.NoError(t, f.safety.Handle(context.Background(), in.RunSafetyRefreshes{}))
	assert.Equal(t, 1, f.gateway.listCount(), "idle: 30 min")

	f.mustSubmit(alice, magnet("active"))
	f.list(true, f.observed("1001", btih("active"), remoteitem.StateDownloading))
	require.NoError(t, f.safety.Handle(context.Background(), in.RunSafetyRefreshes{}))
	assert.Equal(t, 2, f.gateway.listCount(), "active: 5 min")
}

func TestRefresh_read_max_age_counts_from_the_stored_sync(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(true)
	// A complete listing recorded before a restart: the refresher's memory is empty.
	require.NoError(t, f.storage.RecordAuthoritativeSync(context.Background(), mustProvider(t), f.clock.Now()))

	f.clock.Advance(5 * time.Second)
	f.mustRefresh(in.RefreshCauseRead)

	assert.Zero(t, f.gateway.listCount(), "ADR 0009 rule 6")
}

func TestRefresh_forced_refreshes_are_not_paused_by_the_back_off(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.list(false)
	assert.Equal(t, remoteitem.ErrStateStale, f.refreshAs(in.RefreshCauseRead))
	lists := f.gateway.listCount()

	f.list(true)
	f.mustRefresh(in.RefreshCauseForced)

	assert.Equal(t, lists+1, f.gateway.listCount(), "ADR 0009 rule 10")
	f.mustRefresh(in.RefreshCauseRead) // the complete forced listing ended the back-off
}

func TestRefresh_a_tracked_failed_item_is_not_unmanaged(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("broken"))
	f.clock.Advance(time.Second)
	f.list(true, f.observed("1001", btih("broken"), remoteitem.StateFailed))
	f.mustRefresh(in.RefreshCauseSafety)
	require.Equal(t, "failed", f.job(jobID).State)

	f.clock.Advance(time.Minute)
	f.list(true, f.observed("1001", btih("broken"), remoteitem.StateFailed))
	f.mustRefresh(in.RefreshCauseSafety)

	unmanaged, err := f.storage.UnmanagedItems(context.Background())
	require.NoError(t, err)
	assert.Empty(t, unmanaged, "ADR 0019")
	assert.Equal(t, "failed", f.job(jobID).State)
}
