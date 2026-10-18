package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

func testConfig(c *clock.Fixed) Config {
	return Config{
		UserAgent:             "debroxy-test/1",
		ResponseHeaderTimeout: 5 * time.Second,
		Egress:                Egress{Family: FamilyIPv4},
		Retry:                 RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond},
		CircuitBreaker:        CircuitBreakerConfig{FailureThreshold: 3, OpenDuration: 30 * time.Second},
		Budgets:               map[string]BucketConfig{"api": {Requests: 100, Per: time.Minute}},
		Clock:                 c,
		Logger:                slog.New(slog.DiscardHandler),
		Sleep:                 advancingSleeper(c),
	}
}

func newTestClient(t *testing.T, mutate func(*Config)) (*Client, *clock.Fixed) {
	t.Helper()
	c := clock.NewFixed(start)
	cfg := testConfig(c)
	if mutate != nil {
		mutate(&cfg)
	}
	client, err := New(cfg)
	require.NoError(t, err)
	return client, c
}

func mustSecretURL(t *testing.T, raw string) SecretURL {
	t.Helper()
	u, err := ParseSecretURL(raw)
	require.NoError(t, err)
	return u
}

func TestClient_sends_the_user_agent(t *testing.T) {
	t.Parallel()
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { userAgent = r.UserAgent() }))
	defer server.Close()
	client, _ := newTestClient(t, nil)

	response, err := client.Do(context.Background(), Request{
		Method: http.MethodGet, URL: mustSecretURL(t, server.URL),
		Header: http.Header{"User-Agent": {"client-supplied"}},
	})

	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, "debroxy-test/1", userAgent, "the configured User-Agent always wins (DEC-14)")
}

func TestClient_retries_only_idempotent_calls(t *testing.T) {
	t.Parallel()

	for _, idempotent := range []bool{true, false} {
		t.Run(fmt.Sprintf("idempotent_%v", idempotent), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) < 3 {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			client, _ := newTestClient(t, nil)

			response, err := client.Do(context.Background(), Request{
				Method: http.MethodPost, URL: mustSecretURL(t, server.URL), Body: []byte(`{"x":1}`),
				Idempotent: idempotent, MaxWait: time.Minute,
			})

			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			if idempotent {
				assert.Equal(t, http.StatusOK, response.StatusCode)
				assert.Equal(t, int32(3), calls.Load())
			} else {
				assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode, "creates are sent once (NFR-09)")
				assert.Equal(t, int32(1), calls.Load())
			}
		})
	}
}

func TestClient_honours_retry_after_within_max_wait(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "20")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()

	short, _ := newTestClient(t, nil)
	response, err := short.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL), Idempotent: true, MaxWait: 5 * time.Second})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusTooManyRequests, response.StatusCode, "20 s does not fit a 5 s wait")

	calls.Store(0)
	long, c := newTestClient(t, nil)
	response, err = long.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL), Idempotent: true, MaxWait: time.Minute})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, start.Add(20*time.Second), c.Now())
}

func TestClient_circuit_breaker_opens_and_recovers(t *testing.T) {
	t.Parallel()
	var failing atomic.Bool
	failing.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client, c := newTestClient(t, nil)
	call := func() (*http.Response, error) {
		return client.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL)})
	}

	for range 3 {
		response, err := call()
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	response, err := call()
	closeIfPresent(t, response)
	assert.ErrorIs(t, err, ErrCircuitOpen)

	c.Advance(30 * time.Second)
	failing.Store(false)
	response, err = call()
	require.NoError(t, err, "one trial call after the open period")
	require.NoError(t, response.Body.Close())
	response, err = call()
	require.NoError(t, err, "a successful trial closes the circuit")
	require.NoError(t, response.Body.Close())
}

func TestClient_budget_refusal_sends_nothing(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, _ := newTestClient(t, func(cfg *Config) {
		cfg.Budgets = map[string]BucketConfig{"hourly": {Requests: 2, Per: time.Hour}}
	})
	key := BucketKey{Class: "hourly", Endpoint: "/create"}

	response, err := client.Do(context.Background(), Request{Method: http.MethodPost, URL: mustSecretURL(t, server.URL), Bucket: &key})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	response, err = client.Do(context.Background(), Request{Method: http.MethodPost, URL: mustSecretURL(t, server.URL), Bucket: &key, MaxWait: time.Minute})
	closeIfPresent(t, response)

	var exhausted BudgetExhaustedError
	require.ErrorAs(t, err, &exhausted)
	assert.Equal(t, time.Hour, exhausted.RetryAfter)
	assert.Equal(t, int32(1), calls.Load())
}

func TestClient_errors_never_contain_the_secret_part_of_a_url(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close()) // nothing listens there now
	client, _ := newTestClient(t, func(cfg *Config) { cfg.Retry.MaxAttempts = 1 })
	secretURL := mustSecretURL(t, "http://"+address+"/v1/api/torrents/requestdl?token=SECRET-API-KEY&torrent_id=1")

	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: secretURL})
	closeIfPresent(t, response)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET-API-KEY")
	assert.NotContains(t, err.Error(), "requestdl")
	assert.Contains(t, err.Error(), "http://"+address)
}

func TestClient_pins_the_ip_family(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ipv6Only, _ := newTestClient(t, func(cfg *Config) { cfg.Egress = Egress{Family: FamilyIPv6} })

	response, err := ipv6Only.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL)})
	closeIfPresent(t, response)

	var noAddress NoAddressInFamilyError
	require.ErrorAs(t, err, &noAddress, "an IPv4 server is unreachable from an IPv6-pinned client; no fallback")
	assert.Equal(t, FamilyIPv6, noAddress.Family)
	assert.ErrorAs(t, ipv6Only.Probe(context.Background(), server.Listener.Addr().String()), &noAddress)
}

func TestClient_binds_the_local_address(t *testing.T) {
	t.Parallel()
	var remote string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { remote = r.RemoteAddr }))
	defer server.Close()
	client, _ := newTestClient(t, func(cfg *Config) {
		cfg.Egress = Egress{Family: FamilyIPv4, LocalAddress: netip.MustParseAddr("127.0.0.1")}
	})

	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL)})

	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	host, _, err := net.SplitHostPort(remote)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", host)
}

// Not parallel: it sets process environment variables.
func TestClient_ignores_proxy_environment_variables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	client, _ := newTestClient(t, nil)

	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: mustSecretURL(t, server.URL)})

	require.NoError(t, err, "the dead proxy was not used")
	require.NoError(t, response.Body.Close())
}

func TestSecretURL_prints_only_scheme_and_host(t *testing.T) {
	t.Parallel()
	u, err := url.Parse("https://store-079.wnam.tb-cdn.io/dl/SECRET-TOKEN/file.iso?token=SECRET-KEY")
	require.NoError(t, err)
	secret := NewSecretURL(u)
	asJSON, err := json.Marshal(struct{ U SecretURL }{secret})
	require.NoError(t, err)

	for _, printed := range []string{fmt.Sprint(secret), fmt.Sprintf("%v %+v %#v %s", secret, secret, secret, secret), string(asJSON), secret.LogValue().String()} {
		assert.NotContains(t, printed, "SECRET")
		assert.Contains(t, printed, "https://store-079.wnam.tb-cdn.io")
	}
	assert.Equal(t, "store-079.wnam.tb-cdn.io", secret.Host())

	_, err = ParseSecretURL("::not a url?token=SECRET")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestNew_rejects_invalid_configs(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)

	for name, mutate := range map[string]func(*Config){
		"multi_line_user_agent": func(cfg *Config) { cfg.UserAgent = "a\r\nX-Injected: 1" },
		"no_family":             func(cfg *Config) { cfg.Egress = Egress{} },
		"wrong_local_family":    func(cfg *Config) { cfg.Egress.LocalAddress = netip.MustParseAddr("::1") },
		"zero_attempts":         func(cfg *Config) { cfg.Retry.MaxAttempts = 0 },
	} {
		cfg := testConfig(c)
		mutate(&cfg)
		_, err := New(cfg)
		assert.Error(t, err, name)
	}
}

func TestCheckLocalAddress(t *testing.T) {
	t.Parallel()

	assert.NoError(t, CheckLocalAddress(netip.MustParseAddr("127.0.0.1")))
	assert.Error(t, CheckLocalAddress(netip.MustParseAddr("192.0.2.123")), "TEST-NET-1 is never assigned")
}

// closeIfPresent closes the body of a response that a failing call may still return.
func closeIfPresent(t *testing.T, response *http.Response) {
	t.Helper()
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
}
