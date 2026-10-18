package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync/atomic"
	"time"
)

// ReadinessCheck reports whether a dependency can serve requests.
type ReadinessCheck func(ctx context.Context) error

// Health serves GET /healthz (the process is alive) and GET /readyz (every
// readiness check passes and the process is not shutting down). Neither
// needs authentication, so responses name failed checks but never their
// errors; those go to the log.
type Health struct {
	checks       map[string]ReadinessCheck
	logger       *slog.Logger
	shuttingDown atomic.Bool
}

// readinessCheckTimeout bounds each check so a stuck dependency cannot stall probes.
const readinessCheckTimeout = 2 * time.Second

func NewHealth(checks map[string]ReadinessCheck, logger *slog.Logger) *Health {
	return &Health{checks: checks, logger: logger}
}

// Register adds both endpoints to mux.
func (h *Health) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.serveLiveness)
	mux.HandleFunc("GET /readyz", h.serveReadiness)
}

// MarkShuttingDown makes /readyz fail, so load balancers stop sending new
// requests while running ones finish.
func (h *Health) MarkShuttingDown() { h.shuttingDown.Store(true) }

type healthBody struct {
	Status string   `json:"status"`
	Failed []string `json:"failed,omitempty"`
}

func (h *Health) serveLiveness(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, http.StatusOK, healthBody{Status: "alive"})
}

func (h *Health) serveReadiness(w http.ResponseWriter, r *http.Request) {
	if h.shuttingDown.Load() {
		h.respond(w, r, http.StatusServiceUnavailable, healthBody{Status: "shutting-down"})
		return
	}
	var failed []string
	for name, check := range h.checks {
		ctx, cancel := context.WithTimeout(r.Context(), readinessCheckTimeout)
		err := check(ctx)
		cancel()
		if err != nil {
			h.logger.WarnContext(r.Context(), "readiness check failed", "check", name, "error", err.Error())
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		h.respond(w, r, http.StatusServiceUnavailable, healthBody{Status: "not-ready", Failed: failed})
		return
	}
	h.respond(w, r, http.StatusOK, healthBody{Status: "ready"})
}

func (h *Health) respond(w http.ResponseWriter, r *http.Request, status int, body healthBody) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.DebugContext(r.Context(), "write health response", "error", err.Error())
	}
}
