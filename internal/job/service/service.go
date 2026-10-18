// Package service is the composition root of the job context (ddd-rules
// S-1, S-2): it builds the application and its adapters.
package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	jobhttp "github.com/feruzabad/debroxy/internal/job/adapters/in/http"
	"github.com/feruzabad/debroxy/internal/job/adapters/in/outbox"
	"github.com/feruzabad/debroxy/internal/job/adapters/in/scheduler"
	"github.com/feruzabad/debroxy/internal/job/adapters/in/webhook"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/changefeed"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/identity"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/provider"
	"github.com/feruzabad/debroxy/internal/job/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/job/app/command"
	"github.com/feruzabad/debroxy/internal/job/app/query"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// Config is the job configuration, mapped by cmd from the config file.
type Config struct {
	DefaultSlotQuota        int
	SubmitRatePerHour       int
	CachedSubmitRatePerHour int
	SubmitRequestDeadline   time.Duration
	FollowUpRefreshes       []time.Duration
	SubmissionTimeout       time.Duration
	StallThreshold          time.Duration

	RefreshMaxAge                 time.Duration
	RefreshBackoffInitial         time.Duration
	RefreshBackoffMax             time.Duration
	WebhookRefreshInterval        time.Duration
	ForcedRefreshPerUserPerMinute int
	SafetyRefreshActive           time.Duration
	SafetyRefreshIdle             time.Duration
	AbsenceGrace                  time.Duration
	// AbsenceGraceByProvider overrides AbsenceGrace per provider ID.
	AbsenceGraceByProvider map[string]time.Duration

	OutboxMaxAttempts    int
	OutboxInitialBackoff time.Duration
	OutboxMaxBackoff     time.Duration
	Retention            RetentionConfig

	// EventsMaxStreamsPerUser and EventsMaxStreamsTotal limit open Event
	// Streams (spec §5.11).
	EventsMaxStreamsPerUser int
	EventsMaxStreamsTotal   int
}

// RetentionConfig holds the retention periods of spec §5.8.
type RetentionConfig = command.RetentionConfig

// outboxInterval is how often the outbox processor polls.
const outboxInterval = time.Second

// schedulerInterval is the tick of the periodic commands (spec §5.4: every minute).
const schedulerInterval = time.Minute

// Dependencies are the shared resources cmd creates and closes (S-1).
// Provider and Identity are the supplier contexts' applications (DEP-4).
type Dependencies struct {
	Database *commonsqlite.Database
	Provider providerin.Application
	Identity identityin.Application
	Logger   *slog.Logger
	Metrics  decorator.MetricsClient
	Clock    clock.Clock
}

// Wait blocks until the work that outlives requests has finished. cmd
// calls it during shutdown, after the HTTP server and before the database
// closes (NFR-06).
type Wait func()

// NewApplication wires the production adapters. ctx is the process
// lifetime: detached submissions end with it.
func NewApplication(ctx context.Context, cfg Config, deps Dependencies) (in.Application, Wait, error) {
	if deps.Database == nil || deps.Logger == nil || deps.Metrics == nil || deps.Clock == nil {
		return in.Application{}, nil, errors.New("job: incomplete dependencies")
	}
	stores := sqlite.NewSQLiteJobStores(deps.Database)
	changes := changefeed.NewHub(changefeed.DefaultQueueSize)
	app, background, err := newApplication(ctx, cfg, storage{
		changes:   changes,
		items:     sqlite.NewSQLiteRemoteItemRepository(deps.Database, deps.Clock, changes),
		quotas:    stores,
		syncState: stores,
		unmanaged: stores,
		outbox:    stores,
		retention: stores,
		readModel: stores,
	}, provider.NewProviderGateway(deps.Provider, deps.Clock), identity.NewUserDirectory(deps.Identity), deps)
	if err != nil {
		return in.Application{}, nil, err
	}
	periodic := scheduler.NewScheduler(app.Commands, schedulerInterval)
	background.Go(periodic.Run)
	background.Go(outbox.NewProcessor(app.Commands.ProcessOutbox, outboxInterval).Run)
	return app, background.Wait, nil
}

// NewComponentTestApplication wires memory storage around the given
// provider and identity applications (S-2).
func NewComponentTestApplication(ctx context.Context, cfg Config, providerApp providerin.Application, identityApp identityin.Application, clk clock.Clock, logger *slog.Logger) (in.Application, Wait, error) {
	changes := changefeed.NewHub(changefeed.DefaultQueueSize)
	stores := memory.NewMemoryJobStorage(clk).NotifyChangesTo(changes)
	app, background, err := newApplication(ctx, cfg,
		storage{changes: changes, items: stores, quotas: stores, syncState: stores, unmanaged: stores, outbox: stores, retention: stores, readModel: stores},
		provider.NewProviderGateway(providerApp, clk), identity.NewUserDirectory(identityApp),
		Dependencies{Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clk})
	if err != nil {
		return in.Application{}, nil, err
	}
	return app, background.Wait, nil
}

// RegisterHTTPRoutes mounts the job API and the provider webhook on mux.
func RegisterHTTPRoutes(mux *http.ServeMux, app in.Application, cfg HTTPConfig, logger *slog.Logger) {
	jobhttp.RegisterRoutes(mux, app, jobhttp.Config{
		UploadLimitBytes: cfg.UploadLimitBytes, EventPingInterval: cfg.EventPingInterval, EventSessionCheckInterval: cfg.EventSessionCheckInterval,
	}, logger)
	webhook.RegisterRoutes(mux, app.Commands.HandleProviderWebhook, logger)
}

// HTTPConfig holds the limits of the job API.
type HTTPConfig struct {
	UploadLimitBytes int64
	// EventPingInterval and EventSessionCheckInterval override the Event
	// Stream timings for tests; zero keeps the spec values.
	EventPingInterval         time.Duration
	EventSessionCheckInterval time.Duration
}

// NewComponentTestScheduler returns the scheduler that NewApplication runs,
// for tests that tick it by hand.
func NewComponentTestScheduler(app in.Application) scheduler.Scheduler {
	return scheduler.NewScheduler(app.Commands, schedulerInterval)
}

type jobReadModel interface {
	out.JobReadModel
	out.UnmanagedItemsReadModel
	out.ParkedOutboxMessagesReadModel
}

type storage struct {
	// changes is the Change Feed the repository and the Refresher report to
	// and the Event Streams subscribe to.
	changes   *changefeed.Hub
	items     out.RemoteItemRepository
	quotas    out.SlotQuotaRepository
	syncState out.SyncStateRepository
	unmanaged out.UnmanagedItemRepository
	outbox    out.OutboxRepository
	retention out.RetentionRepository
	readModel jobReadModel
}

func newApplication(ctx context.Context, cfg Config, s storage, gateway out.ProviderGateway, users out.UserDirectory, deps Dependencies) (in.Application, *command.Background, error) {
	if cfg.SubmitRequestDeadline <= 0 || cfg.WebhookRefreshInterval <= 0 || cfg.RefreshBackoffMax <= 0 || cfg.OutboxMaxAttempts <= 0 || cfg.OutboxMaxBackoff <= 0 ||
		cfg.RefreshMaxAge <= 0 || cfg.EventsMaxStreamsPerUser <= 0 || cfg.EventsMaxStreamsTotal <= 0 {
		return in.Application{}, nil, errors.New("job: incomplete configuration")
	}
	logger, metrics, clk := deps.Logger, deps.Metrics, deps.Clock
	background := command.NewBackground(ctx)
	submission := command.SubmissionDependencies{
		Items: s.items, Provider: gateway, Clock: clk,
		Config: command.SubmissionConfig{
			DefaultSlotQuota:        cfg.DefaultSlotQuota,
			SubmitRatePerHour:       cfg.SubmitRatePerHour,
			CachedSubmitRatePerHour: cfg.CachedSubmitRatePerHour,
			RequestDeadline:         cfg.SubmitRequestDeadline,
			ExecutionTimeout:        executionTimeout,
			FollowUps:               cfg.FollowUpRefreshes,
		},
	}
	refresher := command.NewRefresher(command.RefreshPipelineDependencies{
		Submission: submission, Unmanaged: s.unmanaged, SyncState: s.syncState, Changes: s.changes,
		AbsenceGrace: func(provider remoteitem.ProviderID) time.Duration {
			if grace, ok := cfg.AbsenceGraceByProvider[provider.String()]; ok {
				return grace
			}
			return cfg.AbsenceGrace
		},
	}, command.RefreshConfig{
		MaxAge: cfg.RefreshMaxAge, BackoffInitial: cfg.RefreshBackoffInitial, BackoffMax: cfg.RefreshBackoffMax,
		WebhookWindow: cfg.WebhookRefreshInterval, ForcedPerUserPerMinute: cfg.ForcedRefreshPerUserPerMinute,
		SafetyActive: cfg.SafetyRefreshActive, SafetyIdle: cfg.SafetyRefreshIdle, RunTimeout: executionTimeout,
	}, background, logger)
	refresh := command.NewRefreshProviderStateHandler(refresher, logger, metrics)
	deletion := command.DeletionDependencies{
		Items: s.items, Outbox: s.outbox, Provider: gateway, Clock: clk,
		Config: command.OutboxConfig{MaxAttempts: cfg.OutboxMaxAttempts, InitialBackoff: cfg.OutboxInitialBackoff, MaxBackoff: cfg.OutboxMaxBackoff},
	}
	return in.Application{
		Commands: in.Commands{
			SubmitJob:         command.NewSubmitJobHandler(submission, s.readModel, users, background, logger, metrics),
			ExecuteSubmission: command.NewExecuteSubmissionHandler(submission, logger, metrics),
			ReleaseJob:        command.NewReleaseJobHandler(s.items, clk, logger, metrics),
			RenameJob:         command.NewRenameJobHandler(s.items, logger, metrics),
			SetSlotQuota:      command.NewSetSlotQuotaHandler(s.quotas, logger, metrics),

			RefreshProviderState:      refresh,
			HandleProviderWebhook:     command.NewHandleProviderWebhookHandler(gateway, refresher, logger, metrics),
			ExpireSubmissions:         command.NewExpireSubmissionsHandler(s.items, clk, cfg.SubmissionTimeout, logger, metrics),
			ResumeDeferredSubmissions: command.NewResumeDeferredSubmissionsHandler(submission, logger, metrics),
			DetectStalls:              command.NewDetectStallsHandler(s.items, clk, cfg.StallThreshold, logger, metrics),
			RunFollowUpRefreshes:      command.NewRunFollowUpRefreshesHandler(s.items, s.syncState, refresher, logger, metrics),
			RunSafetyRefreshes:        command.NewRunSafetyRefreshesHandler(s.items, gateway, refresher, logger, metrics),

			ExecuteRemoteItemDeletion: command.NewExecuteRemoteItemDeletionHandler(deletion, logger, metrics),
			ProcessOutbox:             command.NewProcessOutboxHandler(deletion, logger, metrics),
			PruneExpiredRecords:       command.NewPruneExpiredRecordsHandler(s.retention, clk, cfg.Retention, logger, metrics),

			ProbeProviderCapabilities: command.NewProbeProviderCapabilitiesHandler(gateway, logger, metrics),
		},
		Queries: in.Queries{
			JobsOfUser:           query.NewJobsOfUserHandler(s.readModel, logger, metrics),
			JobDetails:           query.NewJobDetailsHandler(s.readModel, logger, metrics),
			JobHistory:           query.NewJobHistoryHandler(s.readModel, logger, metrics),
			AllJobs:              query.NewAllJobsHandler(s.readModel, logger, metrics),
			QuotaUsage:           query.NewQuotaUsageHandler(s.readModel, cfg.DefaultSlotQuota, logger, metrics),
			SlotQuotaOf:          query.NewSlotQuotaOfHandler(s.readModel, cfg.DefaultSlotQuota, logger, metrics),
			SubmitRate:           query.NewSubmitRateHandler(s.readModel, cfg.SubmitRatePerHour, cfg.CachedSubmitRatePerHour, clk, logger, metrics),
			CachedAvailability:   query.NewCachedAvailabilityHandler(gateway, logger, metrics),
			DeliveryTarget:       query.NewDeliveryTargetHandler(s.readModel, logger, metrics),
			UnmanagedItems:       query.NewUnmanagedItemsHandler(s.readModel, logger, metrics),
			ParkedOutboxMessages: query.NewParkedOutboxMessagesHandler(s.readModel, logger, metrics),
			WatchJobs: query.NewWatchJobsHandler(query.WatchDependencies{
				Changes: s.changes, Jobs: s.readModel, Provider: gateway, Refresh: refresh, Lifetime: ctx, Logger: logger,
			}, query.WatchConfig{
				MaxStreamsPerUser: cfg.EventsMaxStreamsPerUser, MaxStreamsTotal: cfg.EventsMaxStreamsTotal,
				RefreshInterval: cfg.RefreshMaxAge, DefaultSlotQuota: cfg.DefaultSlotQuota,
			}, metrics),
			CurrentUser:     query.NewCurrentUserHandler(users, logger, metrics),
			Providers:       query.NewProvidersHandler(gateway, logger, metrics),
			ProviderHosters: query.NewProviderHostersHandler(gateway, logger, metrics),
		},
	}, background, nil
}

// executionTimeout bounds one detached create, longer than any provider
// client timeout so the provider classifies its own timeouts.
const executionTimeout = 2 * time.Minute
