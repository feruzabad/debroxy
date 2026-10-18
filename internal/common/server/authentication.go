package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
)

// SessionCookieName is the session cookie of spec §4.3.
const SessionCookieName = "debroxy_session"

// Authenticator resolves a session credential to a principal (DEC-22). The
// implementation lives in identity and is passed in by cmd. A rejected
// credential is reported as a slug error.
type Authenticator interface {
	Authenticate(ctx context.Context, credential string) (auth.Principal, error)
	// Check validates the credential again without touching the session.
	Check(ctx context.Context, credential string) error
}

// AuthenticationConfig configures the auth middleware.
type AuthenticationConfig struct {
	// AllowedOrigin is the origin of the public base URL, e.g.
	// "https://debroxy.example.org". Cookie-authenticated state changes must
	// carry it in their Origin header (spec §4.3).
	AllowedOrigin string
}

// Authenticate reads a Bearer token or the session cookie, resolves it with
// authenticator and stores the principal in the request context. A request
// without credentials passes through anonymously; each HTTP adapter decides
// which operations need a principal. A rejected Bearer token ends the
// request with the authenticator's error. A rejected cookie is treated as
// absent, so a browser holding an expired session can still reach the
// login endpoint; protected operations then answer 401 themselves.
func Authenticate(cfg AuthenticationConfig, authenticator Authenticator, logger *slog.Logger) Middleware {
	if authenticator == nil {
		panic("nil authenticator")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			credential, fromCookie := sessionCredential(r)
			if credential == "" {
				next.ServeHTTP(w, r)
				return
			}
			if fromCookie && !isSafeMethod(r.Method) && r.Header.Get("Origin") != cfg.AllowedOrigin {
				httperr.RespondWithSlugError(w, r, logger,
					commonerrors.NewAuthorizationError("cookie-authenticated changes need a matching Origin header", "forbidden"))
				return
			}
			principal, err := authenticator.Authenticate(r.Context(), credential)
			if err != nil {
				if fromCookie && isAuthenticationError(err) {
					next.ServeHTTP(w, r)
					return
				}
				httperr.RespondWithSlugError(w, r, logger, err)
				return
			}
			ctx := auth.WithPrincipal(r.Context(), principal)
			ctx = auth.WithCredentialCheck(ctx, func(ctx context.Context) error { return authenticator.Check(ctx, credential) })
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sessionCredential prefers the Authorization header over the cookie.
func sessionCredential(r *http.Request) (credential string, fromCookie bool) {
	if header := r.Header.Get("Authorization"); header != "" {
		scheme, token, found := strings.Cut(header, " ")
		if found && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token), false
		}
		return "", false
	}
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		return cookie.Value, true
	}
	return "", false
}

// isAuthenticationError separates a rejected credential from a failure of
// the authenticator itself (e.g. the database), which must not be hidden.
func isAuthenticationError(err error) bool {
	var slugErr commonerrors.SlugError
	return errors.As(err, &slugErr) && slugErr.ErrorType() == commonerrors.ErrorTypeAuthentication
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
