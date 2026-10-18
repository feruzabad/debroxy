package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// Event Stream timing (spec §5.11, C14-07).
const (
	defaultPingInterval         = 25 * time.Second
	defaultSessionCheckInterval = 60 * time.Second
	// reconnectDelayMillis is the client's reconnect delay (retry: field).
	reconnectDelayMillis = 3000
)

// eventStream serves GET /api/v1/events as server-sent events. It is not
// generated: oapi-codegen has no streaming responses (watchJobs is excluded
// in oapi-codegen.yml).
type eventStream struct {
	app                  in.Application
	logger               *slog.Logger
	respondWithError     func(w http.ResponseWriter, r *http.Request, err error)
	pingInterval         time.Duration
	sessionCheckInterval time.Duration
}

func (s eventStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFromContext(r.Context())
	if err != nil {
		s.respondWithError(w, r, err)
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = in.WatchScopeOwn
	}
	// The watch ends with this context: when the client goes away, the
	// session check fails, or the stream cannot be written.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	watch, err := s.app.Queries.WatchJobs.Handle(ctx, in.WatchJobs{Actor: actor, Scope: scope})
	if err != nil {
		s.respondWithError(w, r, err)
		return
	}
	defer func() {
		// Wait for the watch to stop reading before the handler returns, so
		// shutdown never closes the database under it.
		cancel()
		for range watch.Events {
		}
	}()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	writer := &sseWriter{w: w, controller: http.NewResponseController(w)}
	writer.raw(fmt.Sprintf("retry: %d\n\n", reconnectDelayMillis))
	writer.event("ready", map[string]string{"scope": scope})
	if writer.flush() != nil {
		return
	}

	ping := time.NewTicker(s.pingInterval)
	defer ping.Stop()
	sessionCheck := time.NewTicker(s.sessionCheckInterval)
	defer sessionCheck.Stop()
	check, _ := auth.CredentialCheckFromContext(r.Context())
	admin := scope == in.WatchScopeAll
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watch.Events:
			if !ok {
				return
			}
			writeJobEvent(writer, event, admin)
		case <-ping.C:
			writer.raw(": ping\n\n")
		case <-sessionCheck.C:
			if check == nil {
				continue
			}
			if err := check(ctx); err != nil {
				if isAuthenticationFailure(err) || ctx.Err() != nil {
					return // the session ended (C14-07)
				}
				// A failed check (e.g. the database is busy) keeps the stream; the
				// next tick checks again.
				s.logger.WarnContext(ctx, "event stream session check failed", "error", err.Error())
			}
			continue
		}
		if writer.flush() != nil {
			return
		}
	}
}

func isAuthenticationFailure(err error) bool {
	var slugErr commonerrors.SlugError
	return errors.As(err, &slugErr) && slugErr.ErrorType() == commonerrors.ErrorTypeAuthentication
}

func writeJobEvent(writer *sseWriter, event in.JobEvent, admin bool) {
	switch {
	case event.Upserted != nil:
		writer.event("job.upserted", jobViewToResponse(*event.Upserted, admin))
	case event.Released != nil:
		writer.event("job.released", map[string]any{
			"jobId": event.Released.JobID, "historyEntry": historyEntryToResponse(event.Released.HistoryEntry),
		})
	case event.Quota != nil:
		writer.event("quota.updated", QuotaUsage{SlotsUsed: event.Quota.SlotsUsed, MaxActiveJobs: event.Quota.MaxActiveJobs})
	case event.ProviderStale != nil:
		writer.event("provider.stale", map[string]any{"providerId": event.ProviderStale.ProviderID, "stale": event.ProviderStale.Stale})
	}
}

// sseWriter writes events with increasing IDs and keeps the first error.
type sseWriter struct {
	w          http.ResponseWriter
	controller *http.ResponseController
	nextID     int64
	err        error
}

func (s *sseWriter) raw(text string) {
	if s.err == nil {
		_, s.err = fmt.Fprint(s.w, text)
	}
}

func (s *sseWriter) event(name string, data any) {
	encoded, err := json.Marshal(data)
	if err != nil {
		s.err = err
		return
	}
	s.nextID++
	s.raw(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", s.nextID, name, encoded))
}

func (s *sseWriter) flush() error {
	if s.err == nil {
		s.err = s.controller.Flush()
	}
	return s.err
}
