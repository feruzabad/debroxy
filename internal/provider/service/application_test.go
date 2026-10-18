package service_test

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/service"
	"github.com/feruzabad/debroxy/migrations"
)

// localHost listens on a loopback port, so the startup egress check never
// leaves the machine.
func localHost(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func torboxAccount(t *testing.T) service.AccountConfig {
	t.Helper()
	host := localHost(t)
	return service.AccountConfig{
		ID: "torbox", Kind: "torbox", DisplayName: "TorBox", Enabled: true,
		APIKey: "key", UserAgent: "debroxy/0.1", BaseURL: "https://" + host + "/v1/api", Timeout: 30 * time.Second,
		CDNLinkCacheMax: 30 * time.Minute, ProbeHosts: []string{host},
		RetryMaxAttempts: 3, RetryBaseDelay: 500 * time.Millisecond, BreakerFailureThreshold: 5, BreakerOpenDuration: 30 * time.Second,
		IPFamily: "ipv4",
		RateBudgets: map[string]service.RateBudget{
			"general": {Requests: 300, Per: time.Minute}, "uncachedCreate": {Requests: 60, Per: time.Hour}, "cdnZip": {Requests: 60, Per: time.Minute},
		},
		WebhookEnabled: true, WebhookSecret: "0123456789abcdef0123456789abcdef",
	}
}

func dependencies(t *testing.T) service.Dependencies {
	t.Helper()
	database, err := commonsqlite.Open(context.Background(), commonsqlite.Config{Path: filepath.Join(t.TempDir(), "provider.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, commonsqlite.Migrate(context.Background(), database, migrations.Files))
	return service.Dependencies{Database: database, Logger: slog.New(slog.DiscardHandler), Metrics: decorator.NoOpMetricsClient{}, Clock: clock.System{}}
}

func TestNewApplication_builds_the_configured_accounts(t *testing.T) {
	t.Parallel()
	realDebrid := torboxAccount(t)
	realDebrid.ID, realDebrid.Kind = "realdebrid", "realdebrid"
	realDebrid.RateBudgets = map[string]service.RateBudget{"general": {Requests: 250, Per: time.Minute}}
	realDebrid.WebhookEnabled = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stops the history sweep

	app, err := service.NewApplication(ctx, service.Config{Accounts: []service.AccountConfig{torboxAccount(t), realDebrid}}, dependencies(t))

	require.NoError(t, err)
	providers, err := app.Operations.ListProviders.Handle(context.Background(), in.ListProvidersRequest{})
	require.NoError(t, err)
	require.Len(t, providers, 2)
	assert.Equal(t, []string{"torbox", "realdebrid"}, []string{providers[0].ID, providers[1].ID})
	assert.Contains(t, providers[0].Capabilities, in.CapabilityView{Name: "SubmitNZBFile", Supported: true, Enabled: false, State: "unverified"})
	assert.Contains(t, providers[1].Capabilities, in.CapabilityView{Name: "SubmitWebURL", Supported: true, Enabled: false, State: "available"})
	assert.Contains(t, providers[1].Capabilities, in.CapabilityView{Name: "CachedCheck", Supported: false, Enabled: false, State: ""})
}

func TestNewApplication_rejects_impossible_configuration(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*service.AccountConfig){
		"unknown_capability": func(a *service.AccountConfig) { a.DisabledCapabilities = []string{"Teleport"} },
		"unknown_kind":       func(a *service.AccountConfig) { a.Kind = "premiumize" },
		"bad_family":         func(a *service.AccountConfig) { a.IPFamily = "both" },
		"missing_budget":     func(a *service.AccountConfig) { delete(a.RateBudgets, "cdnZip") },
		"unreachable_cdn":    func(a *service.AccountConfig) { a.ProbeHosts = []string{"127.0.0.1:1"} },
		"unassigned_address": func(a *service.AccountConfig) { a.LocalAddress = "192.0.2.123" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			account := torboxAccount(t)
			mutate(&account)

			_, err := service.NewApplication(context.Background(), service.Config{Accounts: []service.AccountConfig{account}}, dependencies(t))

			assert.Error(t, err)
		})
	}
}
