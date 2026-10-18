package service

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/httpclient"
	commonsqlite "github.com/feruzabad/debroxy/internal/common/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/realdebrid"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/sqlite"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/torbox"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

// RateBudget is a static budget from configuration.
type RateBudget struct {
	Requests int
	Per      time.Duration
}

// AccountConfig is one configured Provider Account, mapped by cmd.
type AccountConfig struct {
	ID                      string
	Kind                    string
	DisplayName             string
	Enabled                 bool
	APIKey                  string
	UserAgent               string
	BaseURL                 string
	Timeout                 time.Duration
	RetryMaxAttempts        int
	RetryBaseDelay          time.Duration
	BreakerFailureThreshold int
	BreakerOpenDuration     time.Duration
	IPFamily                string
	LocalAddress            string
	RateBudgets             map[string]RateBudget
	DisabledCapabilities    []string
	WebhookEnabled          bool
	WebhookSecret           string
	CDNLinkCacheMax         time.Duration
	// ProbeHosts replaces the adapter's CDN hosts of the startup egress check.
	ProbeHosts []string
}

// Config is the provider configuration.
type Config struct {
	Accounts []AccountConfig
}

// Dependencies are the shared resources cmd creates and closes (S-1).
type Dependencies struct {
	Database *commonsqlite.Database
	Logger   *slog.Logger
	Metrics  decorator.MetricsClient
	Clock    clock.Clock
	// RootCAs replaces the system trust store for provider traffic; nil in
	// production. Full-stack tests set it to trust their TLS fakes.
	RootCAs *x509.CertPool
}

// NewApplication builds one gateway per account, runs the startup egress
// check for every enabled account (ADR 0012 rule 6) and loads the stored
// capability states (ADR 0003 rules 8, 9, 11). Background work of the
// adapters (the Real-Debrid download history sweep, DEC-25) runs until ctx
// ends; cmd passes the process context.
func NewApplication(ctx context.Context, cfg Config, deps Dependencies) (in.Application, error) {
	if deps.Database == nil || deps.Logger == nil || deps.Metrics == nil || deps.Clock == nil {
		return in.Application{}, errors.New("provider: incomplete dependencies")
	}
	var accounts []*account.ProviderAccount
	gateways := out.ProviderAPIGateways{}
	secrets := map[string]string{}
	for _, accountConfig := range cfg.Accounts {
		gateway, err := newGateway(ctx, accountConfig, deps)
		if err != nil {
			return in.Application{}, fmt.Errorf("provider %s: %w", accountConfig.ID, err)
		}
		if accountConfig.Enabled {
			if err := checkEgress(ctx, accountConfig, gateway); err != nil {
				return in.Application{}, err
			}
		}
		definition, err := definitionOf(accountConfig, gateway)
		if err != nil {
			return in.Application{}, err
		}
		stored, err := sqlite.LoadCapabilityStates(ctx, deps.Database, definition.ID)
		if err != nil {
			return in.Application{}, err
		}
		providerAccount, err := account.NewProviderAccount(definition, stored)
		if err != nil {
			return in.Application{}, err
		}
		accounts = append(accounts, providerAccount)
		gateways[accountConfig.ID] = gateway
		if accountConfig.WebhookEnabled {
			secrets[accountConfig.ID] = accountConfig.WebhookSecret
		}
	}
	repository := sqlite.NewSQLiteProviderAccountRepository(deps.Database, accounts)
	return newApplication(repository, gateways, secrets, deps.Clock, deps.Logger, deps.Metrics), nil
}

// newGateway builds the adapter for the account's kind.
func newGateway(ctx context.Context, cfg AccountConfig, deps Dependencies) (out.ProviderAPIGateway, error) {
	family, err := httpclient.NewFamilyFromString(cfg.IPFamily)
	if err != nil {
		return nil, err
	}
	egress := httpclient.Egress{Family: family}
	if cfg.LocalAddress != "" {
		if egress.LocalAddress, err = netip.ParseAddr(cfg.LocalAddress); err != nil {
			return nil, fmt.Errorf("local address: %w", err)
		}
	}
	logger := deps.Logger.With("context", "provider")
	retry := httpclient.RetryConfig{MaxAttempts: cfg.RetryMaxAttempts, BaseDelay: cfg.RetryBaseDelay}
	breaker := httpclient.CircuitBreakerConfig{FailureThreshold: cfg.BreakerFailureThreshold, OpenDuration: cfg.BreakerOpenDuration}
	switch cfg.Kind {
	case account.KindTorBox.String():
		budgets := map[string]torbox.RateBudget{}
		for class, budget := range cfg.RateBudgets {
			budgets[class] = torbox.RateBudget{Requests: budget.Requests, Per: budget.Per}
		}
		return torbox.New(torbox.Config{
			AccountID:       cfg.ID,
			BaseURL:         cfg.BaseURL,
			APIKey:          cfg.APIKey,
			UserAgent:       cfg.UserAgent,
			Timeout:         cfg.Timeout,
			Retry:           retry,
			CircuitBreaker:  breaker,
			Egress:          egress,
			RateBudgets:     budgets,
			CDNLinkCacheMax: cfg.CDNLinkCacheMax,
			ProbeHosts:      cfg.ProbeHosts,
			Clock:           deps.Clock,
			Logger:          logger,
			RootCAs:         deps.RootCAs,
		})
	case account.KindRealDebrid.String():
		budgets := map[string]realdebrid.RateBudget{}
		for class, budget := range cfg.RateBudgets {
			budgets[class] = realdebrid.RateBudget{Requests: budget.Requests, Per: budget.Per}
		}
		gateway, err := realdebrid.New(realdebrid.Config{
			AccountID:       cfg.ID,
			BaseURL:         cfg.BaseURL,
			APIKey:          cfg.APIKey,
			UserAgent:       cfg.UserAgent,
			Timeout:         cfg.Timeout,
			Retry:           retry,
			CircuitBreaker:  breaker,
			Egress:          egress,
			RateBudgets:     budgets,
			CDNLinkCacheMax: cfg.CDNLinkCacheMax,
			ProbeHosts:      cfg.ProbeHosts,
			Clock:           deps.Clock,
			Logger:          logger,
			RootCAs:         deps.RootCAs,
		})
		if err != nil {
			return nil, err
		}
		if cfg.Enabled {
			go gateway.RunDownloadHistorySweep(ctx)
		}
		return gateway, nil
	}
	return nil, fmt.Errorf("unknown provider kind %q", cfg.Kind)
}

// checkEgress: the local address must be assigned to an interface, and the
// API and CDN probe hosts must be reachable in the pinned family. A failure
// stops startup and names the account and host (ADR 0012 rule 6).
func checkEgress(ctx context.Context, cfg AccountConfig, gateway out.ProviderAPIGateway) error {
	if cfg.LocalAddress != "" {
		addr, err := netip.ParseAddr(cfg.LocalAddress)
		if err != nil {
			return fmt.Errorf("provider %s: local address: %w", cfg.ID, err)
		}
		if err := httpclient.CheckLocalAddress(addr); err != nil {
			return fmt.Errorf("provider %s: %w", cfg.ID, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := gateway.CheckEgress(ctx); err != nil {
		return fmt.Errorf("provider %s (%s): %w", cfg.ID, cfg.IPFamily, err)
	}
	return nil
}

func definitionOf(cfg AccountConfig, gateway out.ProviderAPIGateway) (account.Definition, error) {
	id, err := account.NewID(cfg.ID)
	if err != nil {
		return account.Definition{}, err
	}
	kind, err := account.NewKindFromString(cfg.Kind)
	if err != nil {
		return account.Definition{}, err
	}
	disabled := make([]account.Capability, 0, len(cfg.DisabledCapabilities))
	for _, name := range cfg.DisabledCapabilities {
		capability, err := account.NewCapabilityFromString(name)
		if err != nil {
			return account.Definition{}, fmt.Errorf("provider %s: disabledCapabilities: %w", cfg.ID, err)
		}
		disabled = append(disabled, capability)
	}
	return account.Definition{
		ID: id, Kind: kind, DisplayName: cfg.DisplayName, Enabled: cfg.Enabled, WebhooksEnabled: cfg.WebhookEnabled,
		KindCapabilities: gateway.KindCapabilities(), DisabledCapabilities: disabled,
	}, nil
}
