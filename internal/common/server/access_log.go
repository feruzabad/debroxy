package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// redactedPathPrefixes are paths whose remaining segments are secrets: the
// webhook path secret and the Signed Link token (NFR-03).
var redactedPathPrefixes = []string{"/webhooks/", "/dl/"}

// AccessLog writes one line per request. It logs the path without the
// query string and replaces secret path segments.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", RedactedPath(r.URL.Path)),
				slog.Int("status", recorder.statusOrDefault()),
				slog.Int64("bytes", recorder.bytes),
				slog.Duration("duration", time.Since(start)),
			}
			if clientIP, ok := ClientIPFromContext(r.Context()); ok && clientIP.IsValid() {
				attrs = append(attrs, slog.String("client_ip", clientIP.String()))
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request", attrs...)
		})
	}
}

// RedactedPath returns path with every secret segment removed.
func RedactedPath(path string) string {
	for _, prefix := range redactedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return prefix + "<redacted>"
		}
	}
	return path
}

// statusRecorder captures the status and size of a response. Unwrap lets
// http.ResponseController reach Flush and deadlines of the real writer, which
// streaming downloads need.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) statusOrDefault() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
