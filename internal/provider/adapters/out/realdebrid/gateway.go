// Package realdebrid is the Real-Debrid adapter of the provider context:
// it implements ProviderAPIGateway for one Real-Debrid account (ADR 0003).
// Real-Debrid JSON never leaves this package (OUT-3).
package realdebrid

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

// classGeneral is Real-Debrid's only rate class: one bucket for the whole
// API, without rate headers (ADR 0013 rule 5, ASM-R11).
const classGeneral = "general"

const maxResponseBytes = 64 << 20

// RateBudget is a static budget from configuration.
type RateBudget struct {
	Requests int
	Per      time.Duration
}

// Config builds the gateway of one Real-Debrid account.
type Config struct {
	AccountID string
	// BaseURL is the API root, e.g. https://api.real-debrid.com/rest/1.0.
	BaseURL        string
	APIKey         string
	UserAgent      string
	Timeout        time.Duration
	Retry          httpclient.RetryConfig
	CircuitBreaker httpclient.CircuitBreakerConfig
	Egress         httpclient.Egress
	// RateBudgets has the class general.
	RateBudgets map[string]RateBudget
	Clock       clock.Clock
	Logger      *slog.Logger
	Sleep       httpclient.Sleeper
	// PageSize of list walks; zero means 100.
	PageSize int
	// CDNLinkCacheMax bounds how long a resolved CDN link is reused.
	CDNLinkCacheMax time.Duration
	// ProbeHosts replaces the default CDN hosts of the startup egress check
	// ("host" or "host:port"; spec §9.3).
	ProbeHosts []string
	// RootCAs is for tests that serve TLS; nil keeps the system roots.
	RootCAs *x509.CertPool
}

// Gateway talks to the Real-Debrid API for one account.
type Gateway struct {
	accountID string
	baseURL   *url.URL
	apiKey    string
	timeout   time.Duration
	openFor   time.Duration
	pageSize  int
	client    *httpclient.Client
	links     *httpclient.LinkCache
	probes    []string
	clock     clock.Clock
	logger    *slog.Logger

	loggedMu sync.Mutex
	logged   map[string]bool

	// finished caches observations of downloaded and failed items: they no
	// longer change, so listings skip their info call.
	finishedMu sync.Mutex
	finished   map[string]in.Observation
}

var _ out.ProviderAPIGateway = (*Gateway)(nil)

func New(cfg Config) (*Gateway, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("realdebrid: base URL must be absolute")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("realdebrid: empty API key")
	}
	budget, ok := cfg.RateBudgets[classGeneral]
	if !ok {
		return nil, fmt.Errorf("realdebrid: rate budget %q missing", classGeneral)
	}
	client, err := httpclient.New(httpclient.Config{
		UserAgent:             cfg.UserAgent,
		ResponseHeaderTimeout: cfg.Timeout,
		Egress:                cfg.Egress,
		Retry:                 cfg.Retry,
		CircuitBreaker:        cfg.CircuitBreaker,
		Budgets:               map[string]httpclient.BucketConfig{classGeneral: {Requests: budget.Requests, Per: budget.Per}},
		Clock:                 cfg.Clock,
		Logger:                cfg.Logger,
		Sleep:                 cfg.Sleep,
		RootCAs:               cfg.RootCAs,
	})
	if err != nil {
		return nil, fmt.Errorf("realdebrid: %w", err)
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = defaultPageSize
	}
	if cfg.CDNLinkCacheMax <= 0 {
		return nil, errors.New("realdebrid: CDN link cache maximum must be positive")
	}
	probes := cfg.ProbeHosts
	if len(probes) == 0 {
		probes = defaultCDNProbeHosts
	}
	return &Gateway{
		accountID: cfg.AccountID, baseURL: base, apiKey: cfg.APIKey, pageSize: cfg.PageSize,
		timeout: cfg.Timeout, openFor: cfg.CircuitBreaker.OpenDuration,
		client: client, links: httpclient.NewLinkCache(cfg.CDNLinkCacheMax, cfg.Clock), probes: probes, clock: cfg.Clock, logger: cfg.Logger.With("provider", cfg.AccountID),
		logged: map[string]bool{}, finished: map[string]in.Observation{},
	}, nil
}

// KindCapabilities is Real-Debrid's column of spec §6.3. Web links exist at
// the provider but are not enabled in v1 (DEC-16); per-file delivery holds
// only for single-file selections.
func (g *Gateway) KindCapabilities() []account.Capability {
	return []account.Capability{
		account.CapabilitySubmitMagnet, account.CapabilitySubmitTorrentFile, account.CapabilitySubmitWebURL,
		account.CapabilityPerFileDelivery, account.CapabilityBundleDelivery,
	}
}

type apiRequest struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	idempotent  bool
}

type apiResponse struct {
	status int
	header http.Header
	body   []byte
}

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
		Method:     request.method,
		URL:        httpclient.NewSecretURL(target),
		Header:     header,
		Body:       request.body,
		Bucket:     &httpclient.BucketKey{Class: classGeneral},
		MaxWait:    g.timeout,
		Idempotent: request.idempotent,
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
