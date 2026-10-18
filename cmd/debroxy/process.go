package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/cmd/debroxy/config"
	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server"
	"github.com/feruzabad/debroxy/internal/common/sqlite"
	deliveryservice "github.com/feruzabad/debroxy/internal/delivery/service"
	identityservice "github.com/feruzabad/debroxy/internal/identity/service"
	jobservice "github.com/feruzabad/debroxy/internal/job/service"
	providerservice "github.com/feruzabad/debroxy/internal/provider/service"
	"github.com/feruzabad/debroxy/migrations"
	"github.com/feruzabad/debroxy/web"
)

func migrate(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	database, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	logger.InfoContext(ctx, "migrations applied")
	return nil
}

func openDatabase(ctx context.Context, cfg config.Config) (*sqlite.Database, error) {
	database, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.DatabasePath(), ReaderConnections: cfg.Database.ReaderConnections})
	if err != nil {
		return nil, err
	}
	if err := sqlite.Migrate(ctx, database, migrations.Files); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return database, nil
}

// identityConfig maps the config file to the identity context.
func identityConfig(cfg config.Config) identityservice.Config {
	return identityservice.Config{
		SessionAbsoluteLifetime: cfg.Identity.Session.AbsoluteLifetime.Std(),
		SessionIdleTimeout:      cfg.Identity.Session.IdleTimeout.Std(),
	}
}

// deliveryConfig maps the config file to the delivery context.
func deliveryConfig(cfg config.Config) deliveryservice.Config {
	keys := make([]deliveryservice.SigningKey, 0, len(cfg.Delivery.SigningKeys))
	for _, key := range cfg.Delivery.SigningKeys {
		keys = append(keys, deliveryservice.SigningKey{ID: key.ID, Secret: []byte(key.Secret.Reveal())})
	}
	return deliveryservice.Config{
		PublicBaseURL:        cfg.Server.PublicURL.String(),
		SignedLinkTTL:        cfg.Delivery.SignedLinkTTL.Std(),
		MaxStreamsPerUser:    cfg.Delivery.MaxStreamsPerUser,
		MaxConcurrentStreams: cfg.Delivery.MaxConcurrentStreams,
		SigningKeys:          keys,
	}
}

// jobConfig maps the config file to the job context.
func jobConfig(cfg config.Config) jobservice.Config {
	followUps := make([]time.Duration, 0, len(cfg.Job.FollowUpRefreshes))
	for _, after := range cfg.Job.FollowUpRefreshes {
		followUps = append(followUps, after.Std())
	}
	graces := map[string]time.Duration{}
	for _, p := range cfg.Providers {
		if p.AbsenceGrace != nil {
			graces[p.ID] = p.AbsenceGrace.Std()
		}
	}
	return jobservice.Config{
		DefaultSlotQuota:        cfg.Job.DefaultSlotQuota,
		SubmitRatePerHour:       cfg.Job.SubmitRate.PerHour,
		CachedSubmitRatePerHour: cfg.Job.SubmitRate.CachedPerHour,
		SubmitRequestDeadline:   cfg.Job.SubmitRequestDeadline.Std(),
		FollowUpRefreshes:       followUps,
		SubmissionTimeout:       cfg.Job.SubmissionTimeout.Std(),
		StallThreshold:          cfg.Job.StallThreshold.Std(),

		RefreshMaxAge:                 cfg.Job.RefreshMaxAge.Std(),
		RefreshBackoffInitial:         cfg.Job.RefreshBackoff.Initial.Std(),
		RefreshBackoffMax:             cfg.Job.RefreshBackoff.Max.Std(),
		WebhookRefreshInterval:        cfg.Job.WebhookRefreshInterval.Std(),
		ForcedRefreshPerUserPerMinute: cfg.Job.ForcedRefresh.PerUserPerMinute,
		SafetyRefreshActive:           cfg.Job.SafetyRefresh.Active.Std(),
		SafetyRefreshIdle:             cfg.Job.SafetyRefresh.Idle.Std(),
		AbsenceGrace:                  cfg.Job.AbsenceGrace.Std(),
		AbsenceGraceByProvider:        graces,

		OutboxMaxAttempts:       cfg.Job.Outbox.MaxAttempts,
		OutboxInitialBackoff:    cfg.Job.Outbox.InitialBackoff.Std(),
		OutboxMaxBackoff:        cfg.Job.Outbox.MaxBackoff.Std(),
		EventsMaxStreamsPerUser: cfg.Job.Events.MaxStreamsPerUser,
		EventsMaxStreamsTotal:   cfg.Job.Events.MaxStreamsTotal,
		Retention: jobservice.RetentionConfig{
			JobHistory:           cfg.Job.Retention.JobHistory.Std(),
			UnmanagedItemsUnseen: cfg.Job.Retention.UnmanagedItemsUnseen.Std(),
			ProcessedOutbox:      cfg.Job.Retention.ProcessedOutbox.Std(),
			DeletedItems:         cfg.Job.Retention.DeletedItems.Std(),
		},
	}
}

// providerConfig maps the config file to the provider context.
func providerConfig(cfg config.Config) providerservice.Config {
	accounts := make([]providerservice.AccountConfig, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		budgets := map[string]providerservice.RateBudget{}
		for class, budget := range p.RateBudgets {
			budgets[class] = providerservice.RateBudget{Requests: budget.Requests, Per: budget.Per.Std()}
		}
		accounts = append(accounts, providerservice.AccountConfig{
			ID:                      p.ID,
			Kind:                    p.Kind,
			DisplayName:             p.DisplayName,
			Enabled:                 p.IsEnabled(),
			APIKey:                  p.APIKey.Reveal(),
			UserAgent:               p.UserAgent,
			BaseURL:                 p.BaseURL,
			Timeout:                 p.Timeout.Std(),
			RetryMaxAttempts:        p.Retry.MaxAttempts,
			RetryBaseDelay:          p.Retry.BaseDelay.Std(),
			BreakerFailureThreshold: p.CircuitBreaker.FailureThreshold,
			BreakerOpenDuration:     p.CircuitBreaker.OpenDuration.Std(),
			IPFamily:                p.Egress.IPFamily,
			LocalAddress:            p.Egress.LocalAddress,
			RateBudgets:             budgets,
			DisabledCapabilities:    p.DisabledCapabilities,
			WebhookEnabled:          p.Webhook.Enabled,
			WebhookSecret:           p.Webhook.Secret.Reveal(),
			CDNLinkCacheMax:         p.CDNLinkCacheMax.Std(),
			ProbeHosts:              p.CDNProbeHosts,
		})
	}
	return providerservice.Config{Accounts: accounts}
}

func identityDependencies(database *sqlite.Database, logger *slog.Logger) identityservice.Dependencies {
	return identityservice.Dependencies{
		Database: database,
		Logger:   logger,
		Metrics:  decorator.NoOpMetricsClient{},
		Clock:    clock.System{},
	}
}

// bootstrapAdmin creates the first admin; the password comes from stdin.
func bootstrapAdmin(ctx context.Context, cfg config.Config, logger *slog.Logger, username string, stdin io.Reader) (err error) {
	database, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, database.Close()) }()
	identity, err := identityservice.NewApplication(ctx, identityConfig(cfg), identityDependencies(database, logger))
	if err != nil {
		return err
	}
	if err := identityservice.BootstrapAdmin(ctx, identity, username, stdin); err != nil {
		return err
	}
	logger.InfoContext(ctx, "first admin created", "username", username)
	return nil
}

// bootstrapFromEnv creates the first admin from DEBROXY_BOOTSTRAP_ADMIN_*
// before serve listens; when users exist it logs and continues, so
// restarts are safe (spec §11, C12-03).
func bootstrapFromEnv(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	password := strings.NewReader(cfg.Bootstrap.Password.Reveal() + "\n")
	err := bootstrapAdmin(ctx, cfg, logger, cfg.Bootstrap.Username, password)
	var slugErr commonerrors.SlugError
	if errors.As(err, &slugErr) && slugErr.Slug() == bootstrapNotAllowed {
		logger.InfoContext(ctx, "first admin bootstrap skipped: users already exist")
		return nil
	}
	return err
}

// bootstrapNotAllowed is identity's slug for a bootstrap while users exist.
const bootstrapNotAllowed = "bootstrap-not-allowed"

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if cfg.Bootstrap.Username != "" {
		if err := bootstrapFromEnv(ctx, cfg, logger); err != nil {
			return fmt.Errorf("bootstrap first admin: %w", err)
		}
	}
	process, err := startProcess(ctx, cfg, logger)
	if err != nil {
		return err
	}
	return process.run(ctx)
}

// process owns every long-lived resource of `serve`: the database, the HTTP
// listener and server. cmd creates and closes the shared resources (S-1).
type process struct {
	cfg        config.Config
	logger     *slog.Logger
	database   *sqlite.Database
	health     *server.Health
	httpServer *http.Server
	listener   net.Listener
	// waitForJob waits for submissions running past their request.
	waitForJob jobservice.Wait
	// waitForIdentity waits for identity's periodic work.
	waitForIdentity func()
}

func startProcess(ctx context.Context, cfg config.Config, logger *slog.Logger) (*process, error) {
	return startProcessWith(ctx, cfg, logger, processOptions{})
}

// processOptions are seams for full-stack tests; production uses none.
type processOptions struct {
	// rootCAs trusts the TLS fakes that stand in for the providers.
	rootCAs *x509.CertPool
}

func startProcessWith(ctx context.Context, cfg config.Config, logger *slog.Logger, options processOptions) (*process, error) {
	database, err := openDatabase(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Listen before any background work starts, so a failure can still
	// close the database without work running on it.
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.Listen)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("listen on %s: %w", cfg.Server.Listen, err), database.Close())
	}
	closeAll := func(err error) error { return errors.Join(err, listener.Close(), database.Close()) }

	health := server.NewHealth(map[string]server.ReadinessCheck{
		"database": func(ctx context.Context) error { return database.Reader.PingContext(ctx) },
	}, logger)

	identity, err := identityservice.NewApplication(ctx, identityConfig(cfg), identityDependencies(database, logger))
	if err != nil {
		return nil, closeAll(err)
	}
	// Building the provider application validates the provider configuration
	// and checks egress at startup. cmd passes it to job without naming its
	// type (ADR 0014).
	providerApp, err := providerservice.NewApplication(ctx, providerConfig(cfg), providerservice.Dependencies{
		Database: database, Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clock.System{}, RootCAs: options.rootCAs,
	})
	if err != nil {
		return nil, closeAll(err)
	}
	job, waitForJob, err := jobservice.NewApplication(ctx, jobConfig(cfg), jobservice.Dependencies{
		Database: database, Provider: providerApp, Identity: identity, Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clock.System{},
	})
	if err != nil {
		return nil, closeAll(err)
	}

	delivery, err := deliveryservice.NewApplication(ctx, deliveryConfig(cfg), deliveryservice.Dependencies{
		Job: job, Provider: providerApp, Identity: identity, Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clock.System{},
	})
	if err != nil {
		return nil, closeAll(err)
	}

	waitForIdentity := identityservice.StartPeriodicWork(ctx, identity)

	api := http.NewServeMux()
	identityservice.RegisterHTTPRoutes(api, identity, identityservice.HTTPConfig{OIDC: identityservice.OIDCConfig{
		Enabled: cfg.Identity.OIDC.Enabled, IssuerURL: cfg.Identity.OIDC.IssuerURL, ClientID: cfg.Identity.OIDC.ClientID,
		ClientSecret: cfg.Identity.OIDC.ClientSecret.Reveal(), Scopes: cfg.Identity.OIDC.Scopes, PublicBaseURL: cfg.Server.PublicURL.String(),
	}}, logger)
	jobservice.RegisterHTTPRoutes(api, job, jobservice.HTTPConfig{UploadLimitBytes: cfg.Job.UploadLimitBytes}, logger)
	deliveryservice.RegisterHTTPRoutes(api, delivery, logger)
	server.NewClientConfigHandler(server.ClientConfig{
		Version:              version,
		OIDCEnabled:          cfg.Identity.OIDC.Enabled,
		UploadLimitBytes:     cfg.Job.UploadLimitBytes,
		SignedLinkTTLSeconds: int64(cfg.Delivery.SignedLinkTTL.Std() / time.Second),
		PasswordMinLength:    identityservice.PasswordMinLength,
	}).Register(api)
	client, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("browser client: %w", err)
	}
	api.Handle("/", server.NewStaticHandler(client, logger)) // last: every path no API route claims (ADR 0022)
	root := http.NewServeMux()
	health.Register(root)
	root.Handle("/", server.Chain(api,
		server.RequestID(),
		server.ClientIP(cfg.Server.TrustedProxyRange),
		server.AccessLog(logger),
		server.Recover(logger),
		server.Authenticate(server.AuthenticationConfig{AllowedOrigin: cfg.Server.PublicURL.String()},
			identityservice.NewAuthenticator(identity, logger), logger),
	))

	return &process{
		cfg:             cfg,
		logger:          logger,
		database:        database,
		health:          health,
		httpServer:      server.NewHTTPServer(server.Config{Listen: cfg.Server.Listen}, root, logger),
		listener:        listener,
		waitForJob:      waitForJob,
		waitForIdentity: waitForIdentity,
	}, nil
}

// run serves until ctx is cancelled or the server fails, then shuts down.
func (p *process) run(ctx context.Context) error {
	p.logger.InfoContext(ctx, "debroxy started", "listen", p.listener.Addr().String())
	serveErr := make(chan error, 1)
	go func() { serveErr <- p.httpServer.Serve(p.listener) }()

	var err error
	select {
	case <-ctx.Done():
	case err = <-serveErr:
		err = fmt.Errorf("http server: %w", err)
	}
	return errors.Join(err, p.shutdown())
}

// shutdown follows NFR-06: stop being ready, let running requests finish
// within the shutdown timeout, cut the rest (long downloads), then close
// the database and release the start lock.
func (p *process) shutdown() error {
	p.health.MarkShuttingDown()
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Server.ShutdownTimeout.Std())
	defer cancel()

	var errs []error
	if err := p.httpServer.Shutdown(ctx); err != nil {
		p.logger.WarnContext(ctx, "shutdown timeout reached, closing remaining connections", "error", err.Error())
		errs = append(errs, p.httpServer.Close())
	}
	p.waitForJob()
	p.waitForIdentity()
	errs = append(errs, p.database.Close())
	p.logger.InfoContext(ctx, "debroxy stopped")
	return errors.Join(errs...)
}
