package server_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/common/server"
)

func serveHealth(health *server.Health, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	health.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestHealth_readiness(t *testing.T) {
	t.Parallel()
	passing := func(context.Context) error { return nil }
	failing := func(context.Context) error { return errors.New("open /var/lib/debroxy/debroxy.db: disk I/O error") }

	testCases := []struct {
		name         string
		checks       map[string]server.ReadinessCheck
		shuttingDown bool
		wantStatus   int
		wantBody     string
	}{
		{"all_checks_pass", map[string]server.ReadinessCheck{"database": passing}, false, http.StatusOK, `{"status":"ready"}`},
		{"failed_check_is_named", map[string]server.ReadinessCheck{"database": failing, "other": passing}, false, http.StatusServiceUnavailable, `{"status":"not-ready","failed":["database"]}`},
		{"shutting_down", map[string]server.ReadinessCheck{"database": passing}, true, http.StatusServiceUnavailable, `{"status":"shutting-down"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			health := server.NewHealth(tc.checks, slog.New(slog.NewJSONHandler(&logs, nil)))
			if tc.shuttingDown {
				health.MarkShuttingDown()
			}

			response := serveHealth(health, "/readyz")

			assert.Equal(t, tc.wantStatus, response.Code)
			assert.JSONEq(t, tc.wantBody, response.Body.String())
			assert.NotContains(t, response.Body.String(), "disk I/O", "errors go to the log only")
		})
	}
}

func TestHealth_liveness_ignores_failing_checks(t *testing.T) {
	t.Parallel()
	health := server.NewHealth(map[string]server.ReadinessCheck{
		"database": func(context.Context) error { return errors.New("down") },
	}, slog.New(slog.DiscardHandler))
	health.MarkShuttingDown()

	response := serveHealth(health, "/healthz")

	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"status":"alive"}`, response.Body.String())
}
