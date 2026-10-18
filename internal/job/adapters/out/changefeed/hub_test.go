package changefeed_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/job/adapters/out/changefeed"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

func TestHub_fans_out_to_every_subscriber(t *testing.T) {
	t.Parallel()
	hub := changefeed.NewHub(4)
	first, second := hub.Subscribe(), hub.Subscribe()
	torbox, err := remoteitem.NewProviderID("torbox")
	require.NoError(t, err)

	hub.ItemChanged(out.ItemChange{ProviderID: torbox, OwnersAfter: []string{"alice"}})
	hub.ProviderStaleChanged(out.ProviderStaleChange{ProviderID: torbox, Stale: true})

	for _, s := range []out.ChangeSubscription{first, second} {
		item := <-s.Notices()
		require.NotNil(t, item.Item)
		assert.Equal(t, []string{"alice"}, item.Item.OwnersAfter)
		stale := <-s.Notices()
		require.NotNil(t, stale.ProviderStale)
		assert.True(t, stale.ProviderStale.Stale)
	}
}

func TestHub_overruns_a_full_subscriber_without_blocking(t *testing.T) {
	t.Parallel()
	hub := changefeed.NewHub(2)
	slow, fast := hub.Subscribe(), hub.Subscribe()

	for range 3 {
		hub.ItemChanged(out.ItemChange{})
		<-fast.Notices()
	}

	select {
	case <-slow.Overrun():
	default:
		t.Fatal("the full subscriber is not overrun")
	}
	assert.Len(t, slow.Notices(), 2, "the queued notices stay; nothing more arrives")
	hub.ItemChanged(out.ItemChange{})
	assert.Len(t, slow.Notices(), 2)
	assert.Len(t, fast.Notices(), 1)
	select {
	case <-fast.Overrun():
		t.Fatal("a draining subscriber is overrun")
	default:
	}
	assert.Equal(t, 1, hub.Subscribers())
}

func TestHub_close_unsubscribes_and_is_idempotent(t *testing.T) {
	t.Parallel()
	hub := changefeed.NewHub(1)
	s := hub.Subscribe()
	s.Close()
	s.Close()

	hub.ItemChanged(out.ItemChange{})

	assert.Empty(t, s.Notices())
	assert.Zero(t, hub.Subscribers())
}

func TestHub_is_safe_for_concurrent_use(t *testing.T) {
	t.Parallel()
	hub := changefeed.NewHub(changefeed.DefaultQueueSize)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			s := hub.Subscribe()
			defer s.Close()
			for range 50 {
				select {
				case <-s.Notices():
				case <-s.Overrun():
					return
				default:
				}
			}
		})
		wg.Go(func() {
			for range 100 {
				hub.ItemChanged(out.ItemChange{})
				hub.ProviderStaleChanged(out.ProviderStaleChange{})
			}
		})
	}
	wg.Wait()
	assert.Zero(t, hub.Subscribers())
}
