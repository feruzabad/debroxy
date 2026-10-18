package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/feruzabad/debroxy/internal/common/id"
)

// RequestIDHeader carries the request ID back to the client.
const RequestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

type clientIPContextKey struct{}

// RequestID gives every request a new ID (NFR-07). An incoming header is
// ignored, so clients cannot inject IDs into the logs.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := id.New()
			w.Header().Set(RequestIDHeader, requestID)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID)))
		})
	}
}

// RequestIDFromContext returns the ID set by the RequestID middleware.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	return requestID, ok
}

// ClientIP resolves the client address. X-Forwarded-For is trusted only
// when the direct peer is a trusted proxy; then the right-most address that
// is not itself a trusted proxy is the client.
func ClientIP(trustedProxies []netip.Prefix) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := resolveClientIP(r, trustedProxies)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPContextKey{}, clientIP)))
		})
	}
}

// ClientIPFromContext returns the address resolved by the ClientIP middleware.
func ClientIPFromContext(ctx context.Context) (netip.Addr, bool) {
	clientIP, ok := ctx.Value(clientIPContextKey{}).(netip.Addr)
	return clientIP, ok
}

func resolveClientIP(r *http.Request, trustedProxies []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	peer = peer.Unmap()
	if !isTrusted(peer, trustedProxies) {
		return peer
	}

	forwarded := forwardedAddresses(r.Header.Values("X-Forwarded-For"))
	client := peer
	for i := len(forwarded) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(forwarded[i])
		if err != nil {
			break
		}
		client = addr.Unmap()
		if !isTrusted(client, trustedProxies) {
			break
		}
	}
	return client
}

func forwardedAddresses(headerValues []string) []string {
	var addresses []string
	for _, value := range headerValues {
		for part := range strings.SplitSeq(value, ",") {
			addresses = append(addresses, strings.TrimSpace(part))
		}
	}
	return addresses
}

func isTrusted(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// LogHandlerWithRequestID adds the request ID of the record's context to
// every log record, so decorator and adapter logs of one request can be
// joined (NFR-07).
func LogHandlerWithRequestID(handler slog.Handler) slog.Handler {
	return requestIDLogHandler{handler: handler}
}

type requestIDLogHandler struct {
	handler slog.Handler
}

func (h requestIDLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h requestIDLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if requestID, ok := RequestIDFromContext(ctx); ok {
		record = record.Clone()
		record.AddAttrs(slog.String("request_id", requestID))
	}
	return h.handler.Handle(ctx, record)
}

func (h requestIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDLogHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h requestIDLogHandler) WithGroup(name string) slog.Handler {
	return requestIDLogHandler{handler: h.handler.WithGroup(name)}
}
