package query

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// WatchConfig holds the Event Stream settings (spec §5.11).
type WatchConfig struct {
	MaxStreamsPerUser int
	MaxStreamsTotal   int
	// RefreshInterval is job.refreshMaxAge: how often a watch issues its
	// watch refreshes (ADR 0021 rule 8).
	RefreshInterval  time.Duration
	DefaultSlotQuota int
}

// WatchDependencies are what a watch reads and calls.
type WatchDependencies struct {
	Changes  out.ChangeSubscriptions
	Jobs     out.JobReadModel
	Provider out.ProviderGateway
	Refresh  in.RefreshProviderStateHandler
	// Lifetime ends every watch when the service shuts down (ADR 0021 rule 4).
	Lifetime context.Context
	Logger   *slog.Logger
}

// TooManyEventStreamsError: a stream limit is reached (spec §5.11).
type TooManyEventStreamsError struct{ Global bool }

// eventStreamsRetryAfter is the hint of spec §5.11 (ADR 0020).
const eventStreamsRetryAfter = 5 * time.Second

func (e TooManyEventStreamsError) Error() string {
	if e.Global {
		return "too many event streams are open; try again later"
	}
	return "you have too many event streams open; close one first"
}
func (e TooManyEventStreamsError) Slug() string { return "too-many-streams" }
func (e TooManyEventStreamsError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeServiceUnavailable
}
func (e TooManyEventStreamsError) RetryAfter() time.Duration { return eventStreamsRetryAfter }

// eventQueue bounds the events a watch has read but the transport has not
// written; a slow client then fills the feed queue and is overrun.
const eventQueue = 64

type watchJobsHandler struct {
	deps   WatchDependencies
	config WatchConfig
	limits *streamLimits
}

func NewWatchJobsHandler(deps WatchDependencies, config WatchConfig, metricsClient decorator.MetricsClient) in.WatchJobsHandler {
	if deps.Changes == nil || deps.Jobs == nil || deps.Provider == nil || deps.Refresh == nil || deps.Lifetime == nil || deps.Logger == nil {
		panic("incomplete watch dependencies")
	}
	if config.MaxStreamsPerUser <= 0 || config.MaxStreamsTotal <= 0 || config.RefreshInterval <= 0 {
		panic("incomplete watch configuration")
	}
	handler := watchJobsHandler{deps: deps, config: config, limits: &streamLimits{perUser: map[string]int{}}}
	return decorator.ApplyQueryDecorators[in.WatchJobs, in.JobWatch](handler, deps.Logger, metricsClient)
}

// Handle checks the scope and the limits, subscribes, and starts the watch.
// The watch runs until ctx ends or the service shuts down.
func (h watchJobsHandler) Handle(ctx context.Context, query in.WatchJobs) (in.JobWatch, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.JobWatch{}, err
	}
	switch query.Scope {
	case in.WatchScopeOwn:
	case in.WatchScopeAll:
		if err := remoteitem.CanActorAdminister(actor); err != nil {
			return in.JobWatch{}, err
		}
	default:
		return in.JobWatch{}, commonerrors.NewIncorrectInputError("scope must be own or all", "invalid-request")
	}
	if err := h.limits.take(actor.UserID, h.config); err != nil {
		return in.JobWatch{}, err
	}
	events := make(chan in.JobEvent, eventQueue)
	w := &watch{
		handler: h, actor: actor, all: query.Scope == in.WatchScopeAll,
		subscription: h.deps.Changes.Subscribe(), events: events,
	}
	go w.run(ctx)
	return in.JobWatch{Events: events}, nil
}

type streamLimits struct {
	mu      sync.Mutex
	perUser map[string]int
	total   int
}

func (l *streamLimits) take(userID string, config WatchConfig) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= config.MaxStreamsTotal {
		return TooManyEventStreamsError{Global: true}
	}
	if l.perUser[userID] >= config.MaxStreamsPerUser {
		return TooManyEventStreamsError{}
	}
	l.total++
	l.perUser[userID]++
	return nil
}

func (l *streamLimits) release(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total--
	if l.perUser[userID]--; l.perUser[userID] <= 0 {
		delete(l.perUser, userID)
	}
}

type watch struct {
	handler      watchJobsHandler
	actor        remoteitem.Actor
	all          bool
	subscription out.ChangeSubscription
	events       chan<- in.JobEvent
	lastQuota    *in.QuotaUsageResult
}

func (w *watch) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	stopOnShutdown := context.AfterFunc(w.handler.deps.Lifetime, cancel)
	var refreshes sync.WaitGroup
	refreshes.Go(func() { w.refreshLoop(ctx) })
	defer func() {
		// Cancel first so the refresh loop ends, then release everything.
		cancel()
		stopOnShutdown()
		refreshes.Wait()
		w.subscription.Close()
		w.handler.limits.release(w.actor.UserID)
		close(w.events)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.subscription.Overrun():
			return
		case notice := <-w.subscription.Notices():
			if err := w.handle(ctx, notice); err != nil {
				if ctx.Err() == nil && !errors.Is(err, errOverrun) {
					w.handler.deps.Logger.WarnContext(ctx, "event stream read failed", "error", err.Error())
				}
				return
			}
		}
	}
}

func (w *watch) handle(ctx context.Context, notice out.ChangeNotice) error {
	switch {
	case notice.ProviderStale != nil:
		return w.send(ctx, in.JobEvent{ProviderStale: &in.ProviderStaleEvent{
			ProviderID: notice.ProviderStale.ProviderID.String(), Stale: notice.ProviderStale.Stale,
		}})
	case notice.Item != nil:
		return w.itemChanged(ctx, *notice.Item)
	}
	return nil
}

// itemChanged re-reads the item's jobs and emits what the actor may see
// (NFR-04): upserts, releases and, in own scope, a changed quota.
func (w *watch) itemChanged(ctx context.Context, change out.ItemChange) error {
	involved := slices.Contains(change.OwnersBefore, w.actor.UserID) || slices.Contains(change.OwnersAfter, w.actor.UserID)
	if !w.all && !involved {
		return nil
	}
	views, err := w.handler.deps.Jobs.JobsOfItem(ctx, change.RemoteItemID.String())
	if err != nil {
		return err
	}
	for _, view := range views {
		if !w.all && view.OwnerID != w.actor.UserID {
			continue
		}
		upserted := view
		if err := w.send(ctx, in.JobEvent{Upserted: &upserted}); err != nil {
			return err
		}
	}
	for _, released := range change.ReleasedJobs {
		if !w.all && released.OwnerID != w.actor.UserID {
			continue
		}
		entry, found, err := w.handler.deps.Jobs.HistoryEntry(ctx, released.JobID.String())
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := w.send(ctx, in.JobEvent{Released: &in.JobReleasedEvent{JobID: released.JobID.String(), HistoryEntry: entry}}); err != nil {
			return err
		}
	}
	if w.all || !involved {
		return nil
	}
	quota, err := w.handler.deps.Jobs.QuotaUsage(ctx, w.actor.UserID, w.handler.config.DefaultSlotQuota)
	if err != nil {
		return err
	}
	if w.lastQuota != nil && *w.lastQuota == quota {
		return nil
	}
	w.lastQuota = &quota
	return w.send(ctx, in.JobEvent{Quota: &quota})
}

// errOverrun ends a watch whose client fell behind the change feed.
var errOverrun = errors.New("event stream overrun")

func (w *watch) send(ctx context.Context, event in.JobEvent) error {
	select {
	case w.events <- event:
		return nil
	case <-w.subscription.Overrun():
		return errOverrun
	case <-ctx.Done():
		return ctx.Err()
	}
}

// refreshLoop issues watch refreshes for the covered providers every
// RefreshInterval (ADR 0021 rule 8). Single flight and MaxAge collapse the
// refreshes of concurrent watches into one per provider.
func (w *watch) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(w.handler.config.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		providers, err := w.coveredProviders(ctx)
		if err != nil {
			if ctx.Err() == nil {
				w.handler.deps.Logger.WarnContext(ctx, "watch refresh skipped", "error", err.Error())
			}
			continue
		}
		for _, provider := range providers {
			err := w.handler.deps.Refresh.Handle(ctx, in.RefreshProviderState{
				Actor: in.Actor{UserID: w.actor.UserID, Role: w.actor.Role.String()}, ProviderID: provider, Cause: in.RefreshCauseWatch,
			})
			if err != nil && ctx.Err() == nil {
				// The back-off reports itself as provider.stale; nothing else to do.
				w.handler.deps.Logger.DebugContext(ctx, "watch refresh failed", "provider", provider, "error", err.Error())
			}
		}
	}
}

// coveredProviders: in own scope the providers of the actor's items, plus
// every enabled provider while the actor has a submitting item; in scope
// all every enabled provider (ADR 0021 rule 8).
func (w *watch) coveredProviders(ctx context.Context) ([]string, error) {
	var covered []string
	allEnabled := w.all
	if !w.all {
		views, err := w.handler.deps.Jobs.JobsOfUser(ctx, w.actor.UserID)
		if err != nil {
			return nil, err
		}
		for _, view := range views {
			if !slices.Contains(covered, view.ProviderID) {
				covered = append(covered, view.ProviderID)
			}
			if view.State == remoteitem.StateSubmitting.String() {
				allEnabled = true
			}
		}
	}
	if allEnabled {
		providers, err := w.handler.deps.Provider.Providers(ctx)
		if err != nil {
			return nil, err
		}
		for _, provider := range providers {
			if provider.Enabled && !slices.Contains(covered, provider.ID) {
				covered = append(covered, provider.ID)
			}
		}
	}
	return covered, nil
}
