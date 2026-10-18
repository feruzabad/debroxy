// Package server holds the HTTP bootstrap and the generic middleware that
// every context's HTTP adapter runs behind: request IDs, client IP behind
// trusted proxies, access logging with secret redaction, panic recovery and
// authentication (DEC-22). It knows no context.
package server

import (
	"log/slog"
	"net/http"
	"time"
)

// Config is the listener configuration.
type Config struct {
	Listen string
}

// NewHTTPServer returns a server for handler. It sets no write timeout:
// downloads stream for as long as the client reads (spec §7.3).
func NewHTTPServer(cfg Config, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first one listed runs first.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
