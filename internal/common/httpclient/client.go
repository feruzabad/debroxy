package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

// Config builds one client: one per external account (OUT-4).
type Config struct {
	// UserAgent is sent on every request (DEC-14).
	UserAgent string
	// ResponseHeaderTimeout bounds the wait for response headers. Bodies are
	// not bounded, so streams can last as long as the caller reads.
	ResponseHeaderTimeout time.Duration
	Egress                Egress
	Retry                 RetryConfig
	CircuitBreaker        CircuitBreakerConfig
	Budgets               map[string]BucketConfig
	Clock                 clock.Clock
	Logger                *slog.Logger
	// Sleep defaults to SleepWithContext; tests replace it.
	Sleep Sleeper
	// RootCAs replaces the system trust store; nil keeps it. Only tests
	// that serve TLS from httptest set it.
	RootCAs *x509.CertPool
}

// RetryConfig: idempotent calls are tried at most MaxAttempts times, with
// exponential back-off from BaseDelay plus jitter (NFR-09).
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// Client sends requests for one account through its pinned egress.
type Client struct {
	http      *http.Client
	userAgent string
	retry     RetryConfig
	breaker   *circuitBreaker
	budgets   *Budgets
	dialer    pinnedDialer
	sleep     Sleeper
}

func New(cfg Config) (*Client, error) {
	switch {
	case cfg.UserAgent == "" || strings.ContainsAny(cfg.UserAgent, "\r\n"):
		return nil, errors.New("httpclient: user agent must be a non-empty single line")
	case cfg.ResponseHeaderTimeout <= 0:
		return nil, errors.New("httpclient: response header timeout must be positive")
	case cfg.Retry.MaxAttempts < 1 || cfg.Retry.BaseDelay < 0:
		return nil, errors.New("httpclient: retry needs at least one attempt and a non-negative delay")
	case cfg.CircuitBreaker.FailureThreshold < 1 || cfg.CircuitBreaker.OpenDuration <= 0:
		return nil, errors.New("httpclient: circuit breaker needs a positive threshold and duration")
	case cfg.Clock == nil || cfg.Logger == nil:
		return nil, errors.New("httpclient: clock and logger are required")
	}
	if err := cfg.Egress.validate(); err != nil {
		return nil, fmt.Errorf("httpclient: %w", err)
	}
	if cfg.Sleep == nil {
		cfg.Sleep = SleepWithContext
	}
	budgets, err := newBudgets(cfg.Budgets, cfg.Clock, cfg.Sleep, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("httpclient: %w", err)
	}
	dialer := newPinnedDialer(cfg.Egress)
	transport := &http.Transport{
		Proxy:                 nil, // environment proxy variables never apply (ADR 0012 rule 2)
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
		TLSClientConfig:       &tls.Config{RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12},
	}
	return &Client{
		http: &http.Client{
			Transport: transport,
			// Redirects would leave the pinned request description; callers see them.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		userAgent: cfg.UserAgent,
		retry:     cfg.Retry,
		breaker:   &circuitBreaker{cfg: cfg.CircuitBreaker, clock: cfg.Clock},
		budgets:   budgets,
		dialer:    dialer,
		sleep:     cfg.Sleep,
	}, nil
}

// Request describes one call.
type Request struct {
	Method string
	URL    SecretURL
	Header http.Header
	// Body is kept as bytes so a retried call can send it again.
	Body []byte
	// Bucket is the rate bucket the call is charged to; nil means none
	// (e.g. CDN file downloads, ADR 0013 rule 2).
	Bucket *BucketKey
	// AlternativeClasses lets the response headers correct another class of
	// the same endpoint (ADR 0013 rule 6).
	AlternativeClasses []string
	// MaxWait bounds waiting for an empty bucket, a Retry-After or a back-off.
	MaxWait time.Duration
	// Idempotent calls are retried on network errors, 429, 502, 503 and
	// 504. Others are sent once (creates are never retried blindly, NFR-09).
	Idempotent bool
}

// Do sends req and returns the final response; the caller closes its body.
// Non-2xx answers are responses, not errors. Errors never contain the URL
// beyond scheme and host.
func (c *Client) Do(ctx context.Context, req Request) (*http.Response, error) {
	attempts := 1
	if req.Idempotent {
		attempts = c.retry.MaxAttempts
	}
	waited := time.Duration(0)
	for attempt := 1; ; attempt++ {
		response, retryAfter, err := c.send(ctx, req)
		if attempt >= attempts || !shouldRetry(response, err) {
			return response, err
		}
		delay := c.backoff(attempt)
		if retryAfter > 0 {
			delay = retryAfter
		}
		if waited+delay > req.MaxWait {
			return response, err
		}
		if response != nil {
			drainAndClose(response)
		}
		if sleepErr := c.sleep(ctx, delay); sleepErr != nil {
			return nil, sleepErr
		}
		waited += delay
	}
}

func (c *Client) send(ctx context.Context, req Request) (*http.Response, time.Duration, error) {
	if err := c.breaker.allow(); err != nil {
		return nil, 0, err
	}
	var reservation *Reservation
	if req.Bucket != nil {
		var err error
		if reservation, err = c.budgets.Reserve(ctx, *req.Bucket, req.MaxWait); err != nil {
			c.breaker.cancel()
			return nil, 0, err
		}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, req.Method, req.URL.raw(), bytes.NewReader(req.Body))
	if err != nil {
		if reservation != nil {
			reservation.Cancel()
		}
		c.breaker.cancel()
		return nil, 0, fmt.Errorf("build %s request to %s", req.Method, req.URL.Redacted())
	}
	for name, values := range req.Header {
		httpRequest.Header[name] = values
	}
	httpRequest.Header.Set("User-Agent", c.userAgent)

	response, err := c.http.Do(httpRequest)
	if err != nil {
		if reservation != nil {
			reservation.Complete(0, nil)
		}
		c.breaker.record(true)
		return nil, 0, redactTransportError(req, err)
	}
	if reservation != nil {
		reservation.Complete(response.StatusCode, response.Header, req.AlternativeClasses...)
	}
	c.breaker.record(response.StatusCode >= 500)
	retryAfter := time.Duration(0)
	if response.StatusCode == http.StatusTooManyRequests {
		retryAfter, _ = parseRetryAfter(response.Header.Get("Retry-After"))
	}
	return response, retryAfter, nil
}

func shouldRetry(response *http.Response, err error) bool {
	if err != nil {
		var budgetErr BudgetExhaustedError
		var noAddress NoAddressInFamilyError
		switch {
		case errors.As(err, &budgetErr), errors.As(err, &noAddress), errors.Is(err, ErrCircuitOpen),
			errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return false
		}
		return true
	}
	switch response.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (c *Client) backoff(attempt int) time.Duration {
	base := c.retry.BaseDelay << (attempt - 1)
	if base <= 0 {
		return 0
	}
	return base + rand.N(base/2+1) //nolint:gosec // jitter needs no cryptographic randomness
}

func drainAndClose(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
}

// TransportError is a failed call without a response. It names the method
// and the redacted URL only.
type TransportError struct {
	Method string
	URL    string
	Err    error
}

func (e TransportError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Method, e.URL, e.Err)
}

func (e TransportError) Unwrap() error { return e.Err }

// redactTransportError drops the full URL that net/http puts into *url.Error.
func redactTransportError(req Request, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var noAddress NoAddressInFamilyError
	if errors.As(err, &noAddress) {
		return noAddress
	}
	return TransportError{Method: req.Method, URL: req.URL.Redacted(), Err: err}
}

// Probe opens a TCP connection to address ("host:port") through the pinned
// dialer and closes it (ADR 0012 rule 6).
func (c *Client) Probe(ctx context.Context, address string) error {
	conn, err := c.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}
