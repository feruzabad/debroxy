// Package torbox is the TorBox adapter of the provider context: it
// implements ProviderAPIGateway for one TorBox account (ADR 0003). TorBox
// JSON never leaves this package (OUT-3).
package torbox

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/domain/account"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

// Rate classes of spec §9.4 / ADR 0013 rule 3.
const (
	classGeneral        = "general"
	classUncachedCreate = "uncachedCreate"
	classCDNZip         = "cdnZip"
)

// maxResponseBytes bounds API response bodies; listings of large accounts
// are the biggest answers.
const maxResponseBytes = 64 << 20

// hosterCatalogueMaxAge: the catalogue is refreshed every hour (ADR 0003 rule 10).
const hosterCatalogueMaxAge = time.Hour

// RateBudget is a static budget from configuration.
type RateBudget struct {
	Requests int
	Per      time.Duration
}

// Config builds the gateway of one TorBox account.
type Config struct {
	AccountID string
	// BaseURL is the API root, e.g. https://api.torbox.app/v1/api.
	BaseURL        string
	APIKey         string
	UserAgent      string
	Timeout        time.Duration
	Retry          httpclient.RetryConfig
	CircuitBreaker httpclient.CircuitBreakerConfig
	Egress         httpclient.Egress
	// RateBudgets has the classes general, uncachedCreate and cdnZip.
	RateBudgets map[string]RateBudget
	// CDNLinkCacheMax bounds how long a resolved CDN link is reused (ASM-T24).
	CDNLinkCacheMax time.Duration
	// ProbeHosts replaces the default CDN hosts of the startup egress check
	// ("host" or "host:port"; spec §9.3).
	ProbeHosts []string
	Clock      clock.Clock
	Logger     *slog.Logger
	// Sleep is for tests; nil means real sleeping.
	Sleep httpclient.Sleeper
	// RootCAs is for tests that serve TLS; nil keeps the system roots.
	RootCAs *x509.CertPool
	// PageSize of list walks; zero means 1000.
	PageSize int
}

// Gateway talks to the TorBox API for one account.
type Gateway struct {
	accountID string
	baseURL   *url.URL
	apiKey    string
	timeout   time.Duration
	openFor   time.Duration
	client    *httpclient.Client
	links     *httpclient.LinkCache
	probes    []string
	clock     clock.Clock
	logger    *slog.Logger
	pageSize  int

	loggedMu sync.Mutex
	logged   map[string]bool

	hostersMu       sync.Mutex
	hosters         in.HosterCatalogue
	hostersLoadedAt time.Time
}

var _ out.ProviderAPIGateway = (*Gateway)(nil)

func New(cfg Config) (*Gateway, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("torbox: base URL must be absolute")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("torbox: empty API key")
	}
	budgets := map[string]httpclient.BucketConfig{}
	for _, class := range []string{classGeneral, classUncachedCreate, classCDNZip} {
		budget, ok := cfg.RateBudgets[class]
		if !ok {
			return nil, fmt.Errorf("torbox: rate budget %q missing", class)
		}
		budgets[class] = httpclient.BucketConfig{Requests: budget.Requests, Per: budget.Per, ReportsHeaders: true}
	}
	client, err := httpclient.New(httpclient.Config{
		UserAgent:             cfg.UserAgent,
		ResponseHeaderTimeout: cfg.Timeout,
		Egress:                cfg.Egress,
		Retry:                 cfg.Retry,
		CircuitBreaker:        cfg.CircuitBreaker,
		Budgets:               budgets,
		Clock:                 cfg.Clock,
		Logger:                cfg.Logger,
		Sleep:                 cfg.Sleep,
		RootCAs:               cfg.RootCAs,
	})
	if err != nil {
		return nil, fmt.Errorf("torbox: %w", err)
	}
	if cfg.CDNLinkCacheMax <= 0 {
		return nil, errors.New("torbox: CDN link cache maximum must be positive")
	}
	probes := cfg.ProbeHosts
	if len(probes) == 0 {
		probes = defaultCDNProbeHosts
	}
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	return &Gateway{
		accountID: cfg.AccountID, baseURL: base, apiKey: cfg.APIKey,
		timeout: cfg.Timeout, openFor: cfg.CircuitBreaker.OpenDuration,
		client: client, links: httpclient.NewLinkCache(cfg.CDNLinkCacheMax, cfg.Clock), probes: probes,
		clock: cfg.Clock, logger: cfg.Logger.With("provider", cfg.AccountID),
		logged: map[string]bool{}, pageSize: pageSize,
	}, nil
}

// KindCapabilities is TorBox's column of spec §6.3.
func (g *Gateway) KindCapabilities() []account.Capability {
	return []account.Capability{
		account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile,
		account.CapabilitySubmitNZBFile, account.CapabilitySubmitNZBURL, account.CapabilitySubmitWebURL,
		account.CapabilityPerFileDelivery, account.CapabilityBundleDelivery, account.CapabilityCachedCheck,
		account.CapabilityWebhooks, account.CapabilityProviderDedup,
	}
}

// apiRequest describes one API call.
type apiRequest struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	class       string
	// alternatives: other classes the endpoint may be charged to (ADR 0013 rule 6).
	alternatives []string
	idempotent   bool
}

// apiResponse is a response read completely.
type apiResponse struct {
	status int
	header http.Header
	body   []byte
}

// send performs the call within the configured timeout. The API key goes in
// the Authorization header; the URL is a SecretURL anyway.
func (g *Gateway) send(ctx context.Context, request apiRequest) (apiResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	target := g.baseURL.JoinPath(request.path)
	target.RawQuery = request.query.Encode()
	header := http.Header{"Authorization": {"Bearer " + g.apiKey}, "Accept": {"application/json"}}
	if request.contentType != "" {
		header.Set("Content-Type", request.contentType)
	}
	response, err := g.client.Do(ctx, httpclient.Request{
		Method:             request.method,
		URL:                httpclient.NewSecretURL(target),
		Header:             header,
		Body:               request.body,
		Bucket:             &httpclient.BucketKey{Class: request.class, Endpoint: request.path},
		AlternativeClasses: request.alternatives,
		MaxWait:            g.timeout,
		Idempotent:         request.idempotent,
	})
	if err != nil {
		return apiResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return apiResponse{}, fmt.Errorf("read %s response: %w", request.path, err)
	}
	return apiResponse{status: response.StatusCode, header: response.Header, body: body}, nil
}

// logOnce logs an unexpected provider value once per value (NFR-11).
func (g *Gateway) logOnce(key, msg string, args ...any) {
	g.loggedMu.Lock()
	defer g.loggedMu.Unlock()
	if g.logged[key] {
		return
	}
	g.logged[key] = true
	g.logger.Warn(msg, args...)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
