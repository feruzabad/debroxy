package command

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// RefreshConfig holds the refresh settings of spec §5.6.
type RefreshConfig struct {
	// MaxAge: a read refresh is skipped when the last complete listing
	// started this recently.
	MaxAge         time.Duration
	BackoffInitial time.Duration
	BackoffMax     time.Duration
	// WebhookWindow coalesces webhooks: one refresh now, at most one trailing.
	WebhookWindow          time.Duration
	ForcedPerUserPerMinute int
	SafetyActive           time.Duration
	SafetyIdle             time.Duration
	// RunTimeout bounds one pipeline run.
	RunTimeout time.Duration
}

// Refresher coordinates the refreshes of each provider: single flight, the
// back-off after failures (C5-07), the forced-refresh limit (C6-05) and the
// webhook window (C6-06). It is shared by the refresh commands; its state
// is in memory and starts empty after a restart.
type Refresher struct {
	pipeline   refreshPipeline
	clock      clock.Clock
	config     RefreshConfig
	background *Background
	logger     *slog.Logger
	changes    out.ChangeNotifier

	mu        sync.Mutex
	providers map[remoteitem.ProviderID]*providerRefresh
	forced    map[string][]time.Time
}

type providerRefresh struct {
	running        *refreshRun
	lastRun        time.Time // last finished run
	failures       int
	backoffUntil   time.Time
	webhookWindow  bool
	webhookPending bool
}

type refreshRun struct {
	done chan struct{}
	err  error
}

// errIncompleteListing: the listing missed a page or kind; absence is not
// applied and the back-off starts.
var errIncompleteListing = errors.New("incomplete listing")

func NewRefresher(pipeline RefreshPipelineDependencies, config RefreshConfig, background *Background, logger *slog.Logger) *Refresher {
	if background == nil {
		panic("nil background")
	}
	if logger == nil {
		panic("nil logger")
	}
	built := pipeline.build(logger)
	return &Refresher{
		pipeline: built, clock: built.runner.clock, config: config, background: background, logger: logger, changes: pipeline.Changes,
		providers: map[remoteitem.ProviderID]*providerRefresh{}, forced: map[string][]time.Time{},
	}
}

func (r *Refresher) state(provider remoteitem.ProviderID) *providerRefresh {
	state, ok := r.providers[provider]
	if !ok {
		state = &providerRefresh{}
		r.providers[provider] = state
	}
	return state
}

// Refresh runs the pipeline for cause, or joins the run in flight. Reads
// answer remoteitem.ErrStateStale instead of calling the provider during
// the back-off; reads and forced refreshes answer it after a failed or
// incomplete run.
func (r *Refresher) Refresh(ctx context.Context, provider remoteitem.ProviderID, cause, userID string) error {
	userFacing := cause == in.RefreshCauseRead || cause == in.RefreshCauseForced
	// paced: watch refreshes follow the read rules (ADR 0021 rule 9).
	paced := cause == in.RefreshCauseRead || cause == in.RefreshCauseWatch
	var lastSync time.Time
	if paced {
		// MaxAge counts from the stored last complete listing, so it
		// survives restarts (ADR 0009 rule 6).
		var err error
		if lastSync, err = r.pipeline.syncState.LastAuthoritativeSync(ctx, provider); err != nil {
			return err
		}
	}
	r.mu.Lock()
	now := r.clock.Now()
	state := r.state(provider)
	if cause == in.RefreshCauseForced {
		if err := r.takeForced(userID, now); err != nil {
			r.mu.Unlock()
			return err
		}
	}
	switch {
	case paced && !lastSync.IsZero() && now.Sub(lastSync) < r.config.MaxAge:
		r.mu.Unlock()
		return nil
	case paced && now.Before(state.backoffUntil):
		// The back-off suspends only reads and watches; forced refreshes keep
		// their own per-user pacing (ADR 0009 rules 9, 10; ADR 0021 rule 9).
		r.mu.Unlock()
		return remoteitem.ErrStateStale
	}
	run := state.running
	owner := run == nil
	if owner {
		run = &refreshRun{done: make(chan struct{})}
		state.running = run
	}
	r.mu.Unlock()

	if owner {
		// The run is shared, so no caller's cancellation may end it; it ends
		// with the process.
		runCtx, cancel := context.WithTimeout(r.background.ctx, r.config.RunTimeout)
		run.err = r.pipeline.run(runCtx, provider)
		cancel()
		r.finish(provider, run)
	}
	select {
	case <-run.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if run.err != nil && userFacing {
		return remoteitem.ErrStateStale
	}
	return run.err
}

// finish records the outcome: success resets the back-off; a failure or an
// incomplete listing extends it (5 s doubling to 2 min by default). The
// start and the end of a back-off are reported after the lock is released.
func (r *Refresher) finish(provider remoteitem.ProviderID, run *refreshRun) {
	started, ended := r.recordOutcome(provider, run)
	if r.changes != nil && (started || ended) {
		r.changes.ProviderStaleChanged(out.ProviderStaleChange{ProviderID: provider, Stale: started})
	}
}

func (r *Refresher) recordOutcome(provider remoteitem.ProviderID, run *refreshRun) (backoffStarted, backoffEnded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	state := r.state(provider)
	state.running, state.lastRun = nil, now
	if run.err == nil {
		backoffEnded = state.failures > 0
		state.failures, state.backoffUntil = 0, time.Time{}
	} else {
		backoffStarted = state.failures == 0
		state.failures++
		backoff := r.config.BackoffInitial << min(state.failures-1, 30)
		if backoff <= 0 || backoff > r.config.BackoffMax {
			backoff = r.config.BackoffMax
		}
		state.backoffUntil = now.Add(backoff)
		r.logger.Warn("provider refresh failed", "provider", provider.String(), "failures", state.failures, "backoff", backoff.String(), "error", run.err.Error())
	}
	close(run.done)
	return backoffStarted, backoffEnded
}

// takeForced applies the per-user forced-refresh limit over a rolling minute.
func (r *Refresher) takeForced(userID string, now time.Time) error {
	recent := r.forced[userID][:0]
	var oldest time.Time
	for _, at := range r.forced[userID] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
		}
	}
	if len(recent) >= r.config.ForcedPerUserPerMinute {
		r.forced[userID] = recent
		hint := time.Duration(0)
		if !oldest.IsZero() {
			hint = oldest.Add(time.Minute).Sub(now)
		}
		return RefreshRateExceededError{PerMinute: r.config.ForcedPerUserPerMinute, RetryAfterDuration: hint}
	}
	r.forced[userID] = append(recent, now)
	return nil
}

// Webhook refreshes at once on the first webhook of a window and exactly
// once more at the window's end if more arrived; a trailing refresh opens
// a new window (C6-06). The refreshes run in the background.
func (r *Refresher) Webhook(provider remoteitem.ProviderID) {
	r.mu.Lock()
	state := r.state(provider)
	if state.webhookWindow {
		state.webhookPending = true
		r.mu.Unlock()
		return
	}
	state.webhookWindow = true
	r.mu.Unlock()
	r.refreshInBackground(provider)
	r.scheduleWindowEnd(provider)
}

func (r *Refresher) scheduleWindowEnd(provider remoteitem.ProviderID) {
	time.AfterFunc(r.config.WebhookWindow, func() {
		r.mu.Lock()
		state := r.state(provider)
		if !state.webhookPending {
			state.webhookWindow = false
			r.mu.Unlock()
			return
		}
		state.webhookPending = false
		r.mu.Unlock()
		if r.refreshInBackground(provider) {
			r.scheduleWindowEnd(provider)
		}
	})
}

func (r *Refresher) refreshInBackground(provider remoteitem.ProviderID) bool {
	return r.background.Go(func(ctx context.Context) {
		if err := r.Refresh(ctx, provider, in.RefreshCauseWebhook, ""); err != nil {
			r.logger.WarnContext(ctx, "webhook refresh failed", "provider", provider.String(), "error", err.Error())
		}
	})
}

// LastRun is when the provider's last refresh finished (zero if none ran).
func (r *Refresher) LastRun(provider remoteitem.ProviderID) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state(provider).lastRun
}
