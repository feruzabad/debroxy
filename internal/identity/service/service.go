// Package service is the composition root of the identity context
// (ddd-rules S-1, S-2): it builds the application and its driving adapters.
package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/server"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/adapters/in/authenticator"
	"github.com/feruzabad/debroxy/internal/identity/adapters/in/cli"
	identityhttp "github.com/feruzabad/debroxy/internal/identity/adapters/in/http"
	"github.com/feruzabad/debroxy/internal/identity/adapters/in/scheduler"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/argon2"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/memory"
	"github.com/feruzabad/debroxy/internal/identity/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/identity/app/command"
	"github.com/feruzabad/debroxy/internal/identity/app/query"
	"github.com/feruzabad/debroxy/internal/identity/domain/session"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
	"github.com/feruzabad/debroxy/internal/identity/ports/out"
)

// PasswordMinLength is the shortest password identity accepts (spec §4.4);
// cmd publishes it in the client configuration (spec §10.4).
const PasswordMinLength = user.MinimumPasswordLength

// Config is the identity configuration, mapped by cmd from the config file.
type Config struct {
	SessionAbsoluteLifetime time.Duration
	SessionIdleTimeout      time.Duration
}

// Dependencies are the shared resources cmd creates and closes (S-1).
type Dependencies struct {
	Database *commonsqlite.Database
	Logger   *slog.Logger
	Metrics  decorator.MetricsClient
	Clock    clock.Clock
}

// NewApplication wires the production adapters.
func NewApplication(_ context.Context, cfg Config, deps Dependencies) (in.Application, error) {
	if deps.Database == nil || deps.Logger == nil || deps.Metrics == nil || deps.Clock == nil {
		return in.Application{}, errors.New("identity: incomplete dependencies")
	}
	hasher, err := argon2.NewArgon2idPasswordHasher(argon2.DefaultParams)
	if err != nil {
		return in.Application{}, err
	}
	return newApplication(cfg, storage{
		users:     sqlite.NewSQLiteUserRepository(deps.Database),
		sessions:  sqlite.NewSQLiteSessionRepository(deps.Database),
		readModel: sqlite.NewSQLiteIdentityReadModel(deps.Database),
	}, hasher, deps)
}

// NewComponentTestApplication wires memory storage and a cheap hasher for
// component tests (S-2).
func NewComponentTestApplication(cfg Config, clock clock.Clock, logger *slog.Logger) (in.Application, error) {
	hasher, err := argon2.NewArgon2idPasswordHasher(argon2.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltBytes: 16, KeyBytes: 32})
	if err != nil {
		return in.Application{}, err
	}
	users := memory.NewMemoryUserRepository()
	sessions := memory.NewMemorySessionRepository()
	return newApplication(cfg, storage{
		users:     users,
		sessions:  sessions,
		readModel: memory.NewMemoryIdentityReadModel(users, sessions),
	}, hasher, Dependencies{Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clock})
}

type identityReadModel interface {
	out.AuthenticatedActorReadModel
	out.UsersReadModel
	out.UserDetailsReadModel
	out.UserStatusReadModel
}

type storage struct {
	users     out.UserRepository
	sessions  out.SessionRepository
	readModel identityReadModel
}

func newApplication(cfg Config, s storage, hasher out.PasswordHasher, deps Dependencies) (in.Application, error) {
	factory, err := session.NewFactory(session.FactoryConfig{
		AbsoluteLifetime: cfg.SessionAbsoluteLifetime,
		IdleTimeout:      cfg.SessionIdleTimeout,
	})
	if err != nil {
		return in.Application{}, err
	}
	logger, metrics, clk := deps.Logger, deps.Metrics, deps.Clock
	logIn, err := command.NewLogInWithPasswordHandler(s.users, s.sessions, hasher, memory.NewMemoryLoginAttemptLimiter(), factory, clk, logger, metrics)
	if err != nil {
		return in.Application{}, err
	}
	return in.Application{
		Commands: in.Commands{
			BootstrapAdmin:    command.NewBootstrapAdminHandler(s.users, hasher, clk, logger, metrics),
			CreateUser:        command.NewCreateUserHandler(s.users, hasher, clk, logger, metrics),
			ChangeUserRole:    command.NewChangeUserRoleHandler(s.users, logger, metrics),
			DisableUser:       command.NewDisableUserHandler(s.users, logger, metrics),
			EnableUser:        command.NewEnableUserHandler(s.users, logger, metrics),
			ResetUserPassword: command.NewResetUserPasswordHandler(s.users, hasher, logger, metrics),
			ChangeOwnPassword: command.NewChangeOwnPasswordHandler(s.users, s.sessions, hasher, logger, metrics),
			LogInWithPassword: logIn,
			LogOut:            command.NewLogOutHandler(s.sessions, clk, logger, metrics),
			TouchSession:      command.NewTouchSessionHandler(s.sessions, factory, clk, logger, metrics),
			PruneSessions:     command.NewPruneSessionsHandler(s.sessions, clk, logger, metrics),

			LinkOIDCIdentity:   command.NewLinkOIDCIdentityHandler(s.users, logger, metrics),
			UnlinkOIDCIdentity: command.NewUnlinkOIDCIdentityHandler(s.users, logger, metrics),
			LogInWithOIDC:      command.NewLogInWithOIDCHandler(s.users, s.sessions, factory, clk, logger, metrics),
		},
		Queries: in.Queries{
			AuthenticatedActor: query.NewAuthenticatedActorHandler(s.readModel, factory.IdleTimeout(), clk, logger, metrics),
			UserStatus:         query.NewUserStatusHandler(s.readModel, logger, metrics),
			Users:              query.NewUsersHandler(s.readModel, logger, metrics),
			UserDetails:        query.NewUserDetailsHandler(s.readModel, logger, metrics),
		},
	}, nil
}

// pruneInterval is how often dead sessions are deleted (S3-F9).
const pruneInterval = time.Hour

// StartPeriodicWork runs identity's periodic commands until ctx ends. cmd
// calls the returned wait after the HTTP server stops and before the
// database closes (NFR-06).
func StartPeriodicWork(ctx context.Context, app in.Application) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.NewScheduler(app.Commands, pruneInterval).Run(ctx)
	}()
	return func() { <-done }
}

// HTTPConfig holds the settings of the identity API.
type HTTPConfig struct {
	OIDC OIDCConfig
	// Clock checks ID token times; the system clock when nil.
	Clock clock.Clock
}

// OIDCConfig configures the OIDC login (DEC-04). PublicBaseURL builds the
// callback URL and is where a logged-in browser goes.
type OIDCConfig struct {
	Enabled       bool
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	Scopes        []string
	PublicBaseURL string
}

// RegisterHTTPRoutes mounts the identity API on mux.
func RegisterHTTPRoutes(mux *http.ServeMux, app in.Application, cfg HTTPConfig, logger *slog.Logger) {
	base := strings.TrimSuffix(cfg.OIDC.PublicBaseURL, "/")
	oidc := identityhttp.OIDCConfig{
		Enabled: cfg.OIDC.Enabled, IssuerURL: cfg.OIDC.IssuerURL, ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.ClientSecret,
		Scopes: cfg.OIDC.Scopes, RedirectURL: base + "/api/v1/auth/oidc/callback", AfterLoginURL: base + "/",
		LoginErrorURL: base + "/login?error=oidc",
	}
	if cfg.Clock != nil {
		oidc.Now = cfg.Clock.Now
	}
	identityhttp.RegisterRoutes(mux, app, identityhttp.Config{OIDC: oidc}, logger)
}

// NewAuthenticator returns the adapter cmd passes to the auth middleware (DEC-22).
func NewAuthenticator(app in.Application, logger *slog.Logger) server.Authenticator {
	return authenticator.NewAuthenticator(app, logger)
}

// BootstrapAdmin runs the bootstrap-admin command line.
func BootstrapAdmin(ctx context.Context, app in.Application, username string, passwordInput io.Reader) error {
	return cli.BootstrapAdmin(ctx, app, username, passwordInput)
}
