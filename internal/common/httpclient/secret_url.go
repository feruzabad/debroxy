// Package httpclient is debroxy's outbound HTTP client (ADR 0017): pinned
// egress, User-Agent, retries for idempotent calls, a circuit breaker, rate
// buckets driven by x-ratelimit-* headers, and URLs that never print their
// secrets. It knows no provider; callers choose bucket names and decide
// what responses mean.
package httpclient

import (
	"errors"
	"log/slog"
	"net/url"
)

// SecretURL is a URL that may carry a secret (an API key in the query, a
// CDN token in the path). Every way of printing it returns only scheme and
// host (spec §6.5, §7.4); only the client reads the full URL.
type SecretURL struct {
	u *url.URL
}

// NewSecretURL copies u.
func NewSecretURL(u *url.URL) SecretURL {
	copied := *u
	return SecretURL{u: &copied}
}

// ParseSecretURL parses an absolute http(s) URL. A parse error never
// contains the input.
func ParseSecretURL(raw string) (SecretURL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return SecretURL{}, errors.New("not an absolute http(s) URL")
	}
	return SecretURL{u: u}, nil
}

func (s SecretURL) IsZero() bool { return s.u == nil }

// Host is the host name, without port.
func (s SecretURL) Host() string {
	if s.u == nil {
		return ""
	}
	return s.u.Hostname()
}

// Redacted is scheme and host only.
func (s SecretURL) Redacted() string {
	if s.u == nil {
		return "<no url>"
	}
	return s.u.Scheme + "://" + s.u.Host
}

func (s SecretURL) String() string               { return s.Redacted() }
func (s SecretURL) GoString() string             { return s.Redacted() }
func (s SecretURL) LogValue() slog.Value         { return slog.StringValue(s.Redacted()) }
func (s SecretURL) MarshalText() ([]byte, error) { return []byte(s.Redacted()), nil }
func (s SecretURL) MarshalJSON() ([]byte, error) { return []byte(`"` + s.Redacted() + `"`), nil }

func (s SecretURL) raw() string { return s.u.String() }
