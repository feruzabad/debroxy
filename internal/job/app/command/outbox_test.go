package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

func (f *fixture) processOutbox() {
	f.t.Helper()
	require.NoError(f.t, f.outbox.Handle(context.Background(), in.ProcessOutbox{}))
}

func (f *fixture) due() []out.OutboxMessage {
	f.t.Helper()
	messages, err := f.storage.DueMessages(context.Background(), f.clock.Now(), 10)
	require.NoError(f.t, err)
	return messages
}

func TestProcessOutbox_deletes_at_the_provider_and_confirms(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("done"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))

	f.processOutbox()

	assert.Equal(t, []string{"1001"}, f.gateway.deletes())
	item := f.itemByID(itemID)
	assert.Equal(t, remoteitem.StateDeleted, item.State())
	assert.Equal(t, f.clock.Now(), item.DeletedAt())
	assert.Empty(t, f.due(), "message processed")
}

func TestProcessOutbox_retries_with_back_off_and_parks_after_the_last_attempt(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.deleteErr = remoteitem.ProviderUnavailableError{Provider: provider}
	old := f.mustSubmit(alice, magnet("x"))
	itemID := f.job(old).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))
	deferred := f.mustSubmit(bob, magnet("x-variant"))

	f.processOutbox()
	assert.Empty(t, f.due(), "first retry after 1 s")
	f.clock.Advance(time.Second)
	f.processOutbox()
	f.clock.Advance(2 * time.Second)
	f.processOutbox()

	assert.Len(t, f.gateway.deletes(), 3)
	assert.Equal(t, remoteitem.StateDeleting, f.itemByID(itemID).State())
	parked, err := f.storage.ParkedOutboxMessages(context.Background())
	require.NoError(t, err)
	require.Len(t, parked, 1)
	assert.Equal(t, 3, parked[0].Attempts)
	assert.Contains(t, parked[0].LastError, "unavailable")

	f.clock.Advance(time.Hour)
	f.processOutbox()
	assert.Len(t, f.gateway.deletes(), 3, "parked messages stop")
	require.NoError(t, f.expire.Handle(context.Background(), in.ExpireSubmissions{}))
	assert.Equal(t, remoteitem.FailureBlockedByDelete, f.job(deferred).FailureReason, "C5-04")
}

func TestProcessOutbox_converges_when_the_provider_deleted_but_the_answer_was_lost(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.deleteErr, f.gateway.deleteErrOnce = errors.New("connection reset after the delete"), true
	jobID := f.mustSubmit(alice, magnet("lost"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))

	f.processOutbox()
	assert.Equal(t, remoteitem.StateDeleting, f.itemByID(itemID).State())
	f.clock.Advance(time.Second)
	f.processOutbox() // the provider's verified deletion finds the item gone

	assert.Equal(t, remoteitem.StateDeleted, f.itemByID(itemID).State())
	assert.Empty(t, f.due())
}

func TestProcessOutbox_supersedes_when_a_claimable_item_holds_the_provider_item(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustSubmit(alice, magnet("x"))
	itemID := f.job(old).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: old}))
	f.claimableHolding(t, "1001")

	f.processOutbox()

	assert.Empty(t, f.gateway.deletes(), "no provider call")
	assert.Equal(t, remoteitem.StateDeleted, f.itemByID(itemID).State())
	assert.Empty(t, f.due())
}

// claimableHolding stores a ready item holding the provider item, as a
// takeover would leave it.
func (f *fixture) claimableHolding(t *testing.T, providerItemID string) {
	t.Helper()
	source := remoteitem.NewMagnetSource(magnet("takeover").Magnet)
	fingerprint, hash, err := remoteitem.Identify(source)
	require.NoError(t, err)
	jobID, err := remoteitem.NewJobID(id.New())
	require.NoError(t, err)
	job, err := remoteitem.NewJob(jobID, "carol", "carol", "", f.clock.Now())
	require.NoError(t, err)
	itemID, err := remoteitem.NewID(id.New())
	require.NoError(t, err)
	pid, err := remoteitem.NewProviderItemID(providerItemID)
	require.NoError(t, err)
	require.NoError(t, f.storage.JoinOrStart(context.Background(), mustProvider(t), fingerprint,
		func(context.Context, *remoteitem.RemoteItem, out.RemoteItemReads) (out.JoinOrStartResult, error) {
			item, err := remoteitem.StartSubmission(itemID, mustProvider(t), source, fingerprint, hash, job, false, f.clock.Now())
			if err != nil {
				return out.JoinOrStartResult{}, err
			}
			return out.JoinOrStartResult{Item: item}, item.CompleteSubmission(pid, f.observed(providerItemID, hash, remoteitem.StateReady), f.clock.Now())
		}))
}

func TestProcessOutbox_finishes_messages_of_items_no_longer_deleting(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	jobID := f.mustSubmit(alice, magnet("done"))
	itemID := f.job(jobID).RemoteItemID
	require.NoError(t, f.release.Handle(context.Background(), in.ReleaseJob{Actor: alice, JobID: jobID}))
	f.update(itemID, func(item *remoteitem.RemoteItem) error { return item.ConfirmDeleted(f.clock.Now()) })

	f.processOutbox()

	assert.Empty(t, f.gateway.deletes())
	assert.Empty(t, f.due())
}

func TestPruneExpiredRecords_uses_the_configured_retention(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.gateway.create = failing(remoteitem.ProviderRejectedError{Provider: provider, Detail: "bad"})
	jobID, err := f.submitAs(alice, magnet("rejected"))
	require.Error(t, err)

	f.clock.Advance(30 * 24 * time.Hour)
	require.NoError(t, f.prune.Handle(context.Background(), in.PruneExpiredRecords{}))
	f.history(jobID) // history is kept 180 days

	f.clock.Advance(150*24*time.Hour + time.Second)
	require.NoError(t, f.prune.Handle(context.Background(), in.PruneExpiredRecords{}))
	_, found, err := f.storage.HistoryEntry(context.Background(), jobID)
	require.NoError(t, err)
	assert.False(t, found)
}
