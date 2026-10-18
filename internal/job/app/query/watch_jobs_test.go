package query_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/changefeed"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/job/app/query"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type providersOnly struct {
	out.ProviderGateway
	providers []in.ProviderView
}

func (p providersOnly) Providers(context.Context) ([]in.ProviderView, error) { return p.providers, nil }

type refreshRecorder struct {
	mu    sync.Mutex
	calls []in.RefreshProviderState
}

func (r *refreshRecorder) Handle(_ context.Context, cmd in.RefreshProviderState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, cmd)
	return nil
}

func (r *refreshRecorder) providers() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	causes := map[string]string{}
	for _, call := range r.calls {
		causes[call.ProviderID] = call.Cause
	}
	return causes
}

func newWatch(t *testing.T, storage *memory.MemoryJobStorage, hub *changefeed.Hub, refresh *refreshRecorder) in.WatchJobsHandler {
	t.Helper()
	return query.NewWatchJobsHandler(query.WatchDependencies{
		Changes: hub, Jobs: storage, Refresh: refresh, Lifetime: t.Context(), Logger: logger,
		Provider: providersOnly{providers: []in.ProviderView{{ID: "torbox", Enabled: true}, {ID: "realdebrid", Enabled: true}, {ID: "off", Enabled: false}}},
	}, query.WatchConfig{MaxStreamsPerUser: 1, MaxStreamsTotal: 2, RefreshInterval: 20 * time.Millisecond, DefaultSlotQuota: 3}, metrics)
}

func TestWatchJobs_refreshes_the_covered_providers_with_cause_watch(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	readyItem(t, storage, true)
	refresh := &refreshRecorder{}
	watch := newWatch(t, storage, changefeed.NewHub(changefeed.DefaultQueueSize), refresh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	own, err := watch.Handle(ctx, in.WatchJobs{Actor: alice, Scope: in.WatchScopeOwn})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(refresh.providers()) > 0 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, map[string]string{"torbox": in.RefreshCauseWatch}, refresh.providers(), "only the providers of alice's items")
	cancel()
	for range own.Events {
	}

	refresh = &refreshRecorder{}
	watch = newWatch(t, storage, changefeed.NewHub(changefeed.DefaultQueueSize), refresh)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err = watch.Handle(ctx, in.WatchJobs{Actor: admin, Scope: in.WatchScopeAll})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(refresh.providers()) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, map[string]string{"torbox": in.RefreshCauseWatch, "realdebrid": in.RefreshCauseWatch}, refresh.providers(), "every enabled provider")
}

func TestWatchJobs_ends_on_overrun_and_frees_its_slot(t *testing.T) {
	t.Parallel()
	storage := memory.NewMemoryJobStorage(clock.NewFixed(start))
	hub := changefeed.NewHub(1)
	watch := newWatch(t, storage, hub, &refreshRecorder{})

	running, err := watch.Handle(context.Background(), in.WatchJobs{Actor: alice, Scope: in.WatchScopeOwn})
	require.NoError(t, err)
	_, err = watch.Handle(context.Background(), in.WatchJobs{Actor: alice, Scope: in.WatchScopeOwn})
	var limit query.TooManyEventStreamsError
	require.ErrorAs(t, err, &limit)
	assert.False(t, limit.Global)

	// Nobody reads the events, so the stale notices pile up until the
	// subscriber is overrun.
	for range 200 {
		hub.ProviderStaleChanged(out.ProviderStaleChange{})
	}
	drained := make(chan struct{})
	go func() {
		for range running.Events {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("the overrun watch did not end")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = watch.Handle(ctx, in.WatchJobs{Actor: alice, Scope: in.WatchScopeOwn})
	assert.NoError(t, err, "the slot is free again")
}

func TestWatchJobs_scope_all_needs_an_admin(t *testing.T) {
	t.Parallel()
	watch := newWatch(t, memory.NewMemoryJobStorage(clock.NewFixed(start)), changefeed.NewHub(1), &refreshRecorder{})
	_, err := watch.Handle(context.Background(), in.WatchJobs{Actor: bob, Scope: in.WatchScopeAll})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "admin")
}
