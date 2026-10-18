package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server"
)

func newLogger(buffer *bytes.Buffer) *slog.Logger {
	return slog.New(server.LogHandlerWithRequestID(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

func TestAccessLog_never_logs_secret_path_segments_or_query_strings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		target   string
		wantPath string
		secrets  []string
	}{
		{"/webhooks/torbox/s3cr3t-path-value", "/webhooks/<redacted>", []string{"s3cr3t-path-value", "torbox"}},
		{"/dl/eyJ0b2tlbiI.c2lnbmF0dXJl/Sintel.mp4", "/dl/<redacted>", []string{"eyJ0b2tlbiI", "c2lnbmF0dXJl"}},
		{"/api/v1/auth/oidc/callback?code=auth-code-value&state=xyz", "/api/v1/auth/oidc/callback", []string{"auth-code-value"}},
		{"/api/v1/jobs", "/api/v1/jobs", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.wantPath, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			handler := server.Chain(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
				server.RequestID(),
				server.AccessLog(newLogger(&logs)),
			)

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tc.target, nil))

			var line map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &line))
			assert.Equal(t, tc.wantPath, line["path"])
			assert.InDelta(t, http.StatusTeapot, line["status"], 0)
			assert.NotEmpty(t, line["request_id"])
			for _, secret := range tc.secrets {
				assert.NotContains(t, logs.String(), secret)
			}
		})
	}
}

func TestRequestID_sets_a_new_id_and_ignores_the_client_header(t *testing.T) {
	t.Parallel()
	var seen string
	handler := server.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = server.RequestIDFromContext(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(server.RequestIDHeader, "injected")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assert.NotEqual(t, "injected", seen)
	assert.Len(t, seen, 36)
	assert.Equal(t, seen, response.Header().Get(server.RequestIDHeader))
}

func TestClientIP(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	testCases := []struct {
		name         string
		remoteAddr   string
		forwardedFor string
		wantClientIP string
	}{
		{"direct_client", "203.0.113.7:4000", "", "203.0.113.7"},
		{"untrusted_peer_cannot_forward", "203.0.113.7:4000", "198.51.100.1", "203.0.113.7"},
		{"trusted_proxy_forwards", "10.0.0.3:4000", "198.51.100.1", "198.51.100.1"},
		{"rightmost_untrusted_wins", "10.0.0.3:4000", "1.1.1.1, 198.51.100.1, 10.0.0.9", "198.51.100.1"},
		{"invalid_entry_stops_the_walk", "10.0.0.3:4000", "198.51.100.1, garbage", "10.0.0.3"},
		{"ipv4_mapped_ipv6_is_unmapped", "[::ffff:203.0.113.7]:4000", "", "203.0.113.7"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var seen netip.Addr
			handler := server.ClientIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen, _ = server.ClientIPFromContext(r.Context())
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = tc.remoteAddr
			if tc.forwardedFor != "" {
				request.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}

			handler.ServeHTTP(httptest.NewRecorder(), request)

			assert.Equal(t, tc.wantClientIP, seen.String())
		})
	}
}

type fakeAuthenticator struct {
	validToken string
}

func (a fakeAuthenticator) Authenticate(_ context.Context, credential string) (auth.Principal, error) {
	switch credential {
	case a.validToken:
		return auth.Principal{UserID: "user-1", Role: "user"}, nil
	case "broken":
		return auth.Principal{}, errors.New("database unavailable")
	}
	return auth.Principal{}, commonerrors.NewAuthenticationError("session expired", "session-expired")
}

func (a fakeAuthenticator) Check(ctx context.Context, credential string) error {
	_, err := a.Authenticate(ctx, credential)
	return err
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	const origin = "https://debroxy.example.org"

	testCases := []struct {
		name          string
		method        string
		authorization string
		cookie        string
		origin        string
		wantStatus    int
		wantPrincipal bool
	}{
		{"anonymous_passes_through", http.MethodGet, "", "", "", http.StatusOK, false},
		{"valid_bearer", http.MethodPost, "Bearer good", "", "", http.StatusOK, true},
		{"bearer_scheme_is_case_insensitive", http.MethodGet, "bearer good", "", "", http.StatusOK, true},
		{"invalid_bearer_is_rejected", http.MethodGet, "Bearer bad", "", "", http.StatusUnauthorized, false},
		{"bearer_wins_over_cookie", http.MethodGet, "Bearer good", "bad", "", http.StatusOK, true},
		{"cookie_on_safe_method", http.MethodGet, "", "good", "", http.StatusOK, true},
		{"rejected_cookie_continues_anonymously", http.MethodGet, "", "expired", "", http.StatusOK, false},
		{"rejected_cookie_change_continues_anonymously", http.MethodPost, "", "expired", origin, http.StatusOK, false},
		{"authenticator_failure_is_not_hidden", http.MethodGet, "", "broken", "", http.StatusInternalServerError, false},
		{"cookie_change_with_matching_origin", http.MethodPost, "", "good", origin, http.StatusOK, true},
		{"cookie_change_without_origin_is_forbidden", http.MethodDelete, "", "good", "", http.StatusForbidden, false},
		{"cookie_change_with_foreign_origin_is_forbidden", http.MethodPost, "", "good", "https://evil.example", http.StatusForbidden, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var hasPrincipal bool
			handler := server.Authenticate(
				server.AuthenticationConfig{AllowedOrigin: origin},
				fakeAuthenticator{validToken: "good"},
				slog.New(slog.DiscardHandler),
			)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, hasPrincipal = auth.PrincipalFromContext(r.Context())
			}))
			request := httptest.NewRequest(tc.method, "/api/v1/jobs", nil)
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: tc.cookie})
			}
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assert.Equal(t, tc.wantStatus, response.Code)
			assert.Equal(t, tc.wantPrincipal, hasPrincipal)
		})
	}
}

func TestRecover_turns_a_panic_into_a_generic_500(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	handler := server.Recover(newLogger(&logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom with detail")
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NotContains(t, response.Body.String(), "boom")
	assert.Contains(t, logs.String(), "boom with detail")
}

func TestRecover_repanics_on_abort_handler(t *testing.T) {
	t.Parallel()
	handler := server.Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestAccessLog_keeps_the_writer_flushable(t *testing.T) {
	t.Parallel()
	var flushErr error
	handler := server.AccessLog(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		flushErr = http.NewResponseController(w).Flush()
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/dl/x/y", nil))

	require.NoError(t, flushErr)
	assert.True(t, response.Flushed)
	assert.True(t, strings.HasPrefix(response.Body.String(), "chunk"))
}
