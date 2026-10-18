// Command debroxy is the debroxy server. It is configured only by
// DEBROXY_* environment variables (spec §11; see .env.example).
//
//	debroxy serve                               run the server (applies pending migrations first)
//	debroxy migrate                             apply pending migrations and exit
//	debroxy bootstrap-admin --username <name>   create the first admin; password on stdin
//	debroxy healthcheck                         exit 0 if the local server is ready
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/feruzabad/debroxy/cmd/debroxy/config"
	"github.com/feruzabad/debroxy/internal/common/server"
)

const usage = `usage: debroxy <command> [options]

commands:
  serve             run the server (applies pending migrations first)
  migrate           apply pending database migrations and exit
  bootstrap-admin   create the first admin (--username <name>); reads the password from stdin
  healthcheck       exit 0 if the server on DEBROXY_SERVER_LISTEN is ready

Configuration comes from DEBROXY_* environment variables (see .env.example)`

// healthcheckTimeout bounds the healthcheck request (spec §11).
const healthcheckTimeout = 3 * time.Second

// version is the build version served at GET /api/v1/config (spec §10.4).
// It is "dev" unless set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stderr, os.Environ())
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "debroxy:", err)
		os.Exit(1)
	}
}

// errUsage reports a wrong command line.
var errUsage = errors.New(usage)

// run is main without process globals, so tests can drive it. environ is
// the process environment as os.Environ returns it.
func run(ctx context.Context, args []string, stdin io.Reader, stderr io.Writer, environ []string) error {
	if len(args) == 0 {
		return errUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "serve", "migrate", "bootstrap-admin", "healthcheck":
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stderr, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q\n%w", command, errUsage)
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	username := flags.String("username", "", "bootstrap-admin: username of the first admin")
	if err := flags.Parse(rest); err != nil {
		return err
	}
	if command == "healthcheck" {
		return healthcheck(ctx, environ)
	}
	cfg, err := config.FromEnv(environ)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.Log, stderr)

	switch command {
	case "migrate":
		return migrate(ctx, cfg, logger)
	case "bootstrap-admin":
		return bootstrapAdmin(ctx, cfg, logger, *username, stdin)
	}
	return serve(ctx, cfg, logger)
}

// healthcheck asks the local server's /readyz; it reads no configuration
// beyond the listen address, so it needs no secrets (spec §11).
func healthcheck(ctx context.Context, environ []string) error {
	url, err := config.HealthcheckURL(environ)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s answered %d", url, response.StatusCode)
	}
	return nil
}

// newLogger writes JSON logs (NFR-07). Every record of a request carries
// its request ID (S1-F4).
func newLogger(cfg config.Log, w io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.Level)) // validated by config
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(server.LogHandlerWithRequestID(handler))
}
