// Package http is the identity HTTP adapter: the generated server for
// api/openapi/identity.yml plus the mapping to identity commands and
// queries (ddd-rules IN-1, IN-2).
package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/common/server"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

var (
	errAuthenticationRequired = commonerrors.NewAuthenticationError("this operation needs a logged-in user", "authentication-required")
	errNothingToUpdate        = commonerrors.NewIncorrectInputError("set role and/or status", "invalid-request")
)

// Config holds the adapter's settings.
type Config struct {
	OIDC OIDCConfig
}

// RegisterRoutes adds every identity route to mux.
func RegisterRoutes(mux *http.ServeMux, app in.Application, cfg Config, logger *slog.Logger) {
	respondWithError := func(w http.ResponseWriter, r *http.Request, err error) {
		httperr.RespondWithSlugError(w, r, logger, err)
	}
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		httperr.RespondWithSlugError(w, r, logger, commonerrors.NewIncorrectInputError(err.Error(), "invalid-request"))
	}
	strict := NewStrictHandlerWithOptions(httpServer{app: app, oidc: newRelyingParty(cfg.OIDC), logger: logger}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		ResponseErrorHandlerFunc: respondWithError,
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: badRequest})
}

type httpServer struct {
	app    in.Application
	oidc   *relyingParty
	logger *slog.Logger
}

var _ StrictServerInterface = httpServer{}

// actorFromContext converts the principal set by the auth middleware
// (DEP-6). Every protected operation starts here (S1-F3).
func actorFromContext(ctx context.Context) (in.Actor, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return in.Actor{}, errAuthenticationRequired
	}
	return in.Actor{UserID: principal.UserID, Role: principal.Role, SessionID: principal.SessionID}, nil
}

func (s httpServer) LogIn(ctx context.Context, request LogInRequestObject) (LogInResponseObject, error) {
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	clientIP := ""
	if addr, ok := server.ClientIPFromContext(ctx); ok && addr.IsValid() {
		clientIP = addr.String()
	}
	cmd := in.LogInWithPassword{
		Username:     request.Body.Username,
		Password:     request.Body.Password,
		ClientIP:     clientIP,
		SessionID:    id.New(),
		SessionToken: token,
	}
	if err := s.app.Commands.LogInWithPassword.Handle(ctx, cmd); err != nil {
		return nil, err
	}
	started, err := s.app.Queries.AuthenticatedActor.Handle(ctx, in.AuthenticatedActor{SessionToken: token})
	if err != nil {
		return nil, err
	}
	cookie := s.sessionCookie(token, started.SessionAbsoluteExpiresAt)
	return LogIn200JSONResponse{
		Body:    LoginResponse{Token: token, ExpiresAt: started.SessionExpiresAt},
		Headers: LogIn200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (s httpServer) LogOut(ctx context.Context, _ LogOutRequestObject) (LogOutResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.LogOut.Handle(ctx, in.LogOut{Actor: actor}); err != nil {
		return nil, err
	}
	cleared := s.clearedSessionCookie()
	return LogOut204Response{Headers: LogOut204ResponseHeaders{SetCookie: &cleared}}, nil
}

func (s httpServer) ChangeOwnPassword(ctx context.Context, request ChangeOwnPasswordRequestObject) (ChangeOwnPasswordResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.ChangeOwnPassword.Handle(ctx, in.ChangeOwnPassword{
		Actor:           actor,
		CurrentPassword: request.Body.CurrentPassword,
		NewPassword:     request.Body.NewPassword,
	})
	if err != nil {
		return nil, err
	}
	return ChangeOwnPassword204Response{}, nil
}

func (s httpServer) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.app.Queries.Users.Handle(ctx, in.Users{Actor: actor})
	if err != nil {
		return nil, err
	}
	response := UserList{Users: make([]User, 0, len(users))}
	for _, u := range users {
		response.Users = append(response.Users, userSummaryToResponse(u))
	}
	return ListUsers200JSONResponse(response), nil
}

func (s httpServer) CreateUser(ctx context.Context, request CreateUserRequestObject) (CreateUserResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	userID := id.New()
	cmd := in.CreateUser{
		Actor:    actor,
		UserID:   userID,
		Username: request.Body.Username,
		Role:     string(request.Body.Role),
	}
	if request.Body.Password != nil {
		cmd.Password = *request.Body.Password
	}
	if request.Body.OidcIdentity != nil {
		cmd.OIDCIssuer, cmd.OIDCSubject = request.Body.OidcIdentity.Issuer, request.Body.OidcIdentity.Subject
	}
	err = s.app.Commands.CreateUser.Handle(ctx, cmd)
	if err != nil {
		return nil, err
	}
	created, err := s.app.Queries.UserDetails.Handle(ctx, in.UserDetails{Actor: actor, UserID: userID})
	if err != nil {
		return nil, err
	}
	return CreateUser201JSONResponse(userSummaryToResponse(created)), nil
}

func (s httpServer) GetUser(ctx context.Context, request GetUserRequestObject) (GetUserResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	details, err := s.app.Queries.UserDetails.Handle(ctx, in.UserDetails{Actor: actor, UserID: request.UserId})
	if err != nil {
		return nil, err
	}
	return GetUser200JSONResponse(userSummaryToResponse(details)), nil
}

// UpdateUser applies the role change first, then the status change; each
// is its own command and transaction.
func (s httpServer) UpdateUser(ctx context.Context, request UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body.Role == nil && request.Body.Status == nil {
		return nil, errNothingToUpdate
	}
	if request.Body.Role != nil {
		err := s.app.Commands.ChangeUserRole.Handle(ctx, in.ChangeUserRole{Actor: actor, UserID: request.UserId, Role: string(*request.Body.Role)})
		if err != nil {
			return nil, err
		}
	}
	if request.Body.Status != nil {
		switch *request.Body.Status {
		case Disabled:
			err = s.app.Commands.DisableUser.Handle(ctx, in.DisableUser{Actor: actor, UserID: request.UserId})
		case Active:
			err = s.app.Commands.EnableUser.Handle(ctx, in.EnableUser{Actor: actor, UserID: request.UserId})
		default:
			err = commonerrors.NewIncorrectInputError("status must be active or disabled", "invalid-status")
		}
		if err != nil {
			return nil, err
		}
	}
	details, err := s.app.Queries.UserDetails.Handle(ctx, in.UserDetails{Actor: actor, UserID: request.UserId})
	if err != nil {
		return nil, err
	}
	return UpdateUser200JSONResponse(userSummaryToResponse(details)), nil
}

func (s httpServer) ResetUserPassword(ctx context.Context, request ResetUserPasswordRequestObject) (ResetUserPasswordResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.ResetUserPassword.Handle(ctx, in.ResetUserPassword{
		Actor: actor, UserID: request.UserId, NewPassword: request.Body.Password,
	})
	if err != nil {
		return nil, err
	}
	return ResetUserPassword204Response{}, nil
}

// StartOIDCLogin redirects the browser to the OIDC provider.
func (s httpServer) StartOIDCLogin(ctx context.Context, _ StartOIDCLoginRequestObject) (StartOIDCLoginResponseObject, error) {
	redirect, cookie, err := s.oidc.start(ctx)
	if err != nil {
		return nil, err
	}
	return StartOIDCLogin302Response{Headers: StartOIDCLogin302ResponseHeaders{Location: &redirect, SetCookie: &cookie}}, nil
}

// FinishOIDCLogin completes the flow and starts a session like a password
// login; every failure redirects to the login page with error=oidc (C14-02),
// the cause is logged, never put in the URL.
func (s httpServer) FinishOIDCLogin(ctx context.Context, request FinishOIDCLoginRequestObject) (FinishOIDCLoginResponseObject, error) {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	if providerError := deref(request.Params.Error); providerError != "" {
		s.logger.InfoContext(ctx, "OIDC provider refused the login", "error", providerError)
		return s.oidcLoginFailed(), nil
	}
	issuer, subject, err := s.oidc.finish(ctx, deref(request.Params.Code), deref(request.Params.State), deref(request.Params.DebroxyOidc))
	if err != nil {
		if errors.Is(err, errOIDCFailed) || errors.Is(err, errOIDCUnavailable) {
			s.logger.InfoContext(ctx, "OIDC login failed", "error", err.Error())
			return s.oidcLoginFailed(), nil
		}
		return nil, err
	}
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.LogInWithOIDC.Handle(ctx, in.LogInWithOIDC{Issuer: issuer, Subject: subject, SessionID: id.New(), SessionToken: token})
	if err != nil {
		var slugErr commonerrors.SlugError
		if errors.As(err, &slugErr) && slugErr.Slug() == "invalid-credentials" {
			s.logger.InfoContext(ctx, "OIDC login failed", "error", err.Error())
			return s.oidcLoginFailed(), nil
		}
		return nil, err
	}
	started, err := s.app.Queries.AuthenticatedActor.Handle(ctx, in.AuthenticatedActor{SessionToken: token})
	if err != nil {
		return nil, err
	}
	cookie := s.sessionCookie(token, started.SessionAbsoluteExpiresAt)
	location := s.oidc.cfg.AfterLoginURL
	return FinishOIDCLogin302Response{Headers: FinishOIDCLogin302ResponseHeaders{Location: &location, SetCookie: &cookie}}, nil
}

// oidcLoginFailed builds the 302 to {publicBaseURL}/login?error=oidc and
// clears the flow cookie (C14-02).
func (s httpServer) oidcLoginFailed() FinishOIDCLogin302Response {
	location := s.oidc.cfg.LoginErrorURL
	cleared := s.clearedOIDCFlowCookie()
	return FinishOIDCLogin302Response{Headers: FinishOIDCLogin302ResponseHeaders{Location: &location, SetCookie: &cleared}}
}

func (s httpServer) clearedOIDCFlowCookie() string {
	cookie := http.Cookie{
		Name:     oidcCookieName,
		Value:    "",
		Path:     oidcCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

func (s httpServer) LinkOIDCIdentity(ctx context.Context, request LinkOIDCIdentityRequestObject) (LinkOIDCIdentityResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.LinkOIDCIdentity.Handle(ctx, in.LinkOIDCIdentity{
		Actor: actor, UserID: request.UserId, Issuer: request.Body.Issuer, Subject: request.Body.Subject,
	})
	if err != nil {
		return nil, err
	}
	details, err := s.app.Queries.UserDetails.Handle(ctx, in.UserDetails{Actor: actor, UserID: request.UserId})
	if err != nil {
		return nil, err
	}
	return LinkOIDCIdentity200JSONResponse(userSummaryToResponse(details)), nil
}

func (s httpServer) UnlinkOIDCIdentity(ctx context.Context, request UnlinkOIDCIdentityRequestObject) (UnlinkOIDCIdentityResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.UnlinkOIDCIdentity.Handle(ctx, in.UnlinkOIDCIdentity{Actor: actor, UserID: request.UserId}); err != nil {
		return nil, err
	}
	details, err := s.app.Queries.UserDetails.Handle(ctx, in.UserDetails{Actor: actor, UserID: request.UserId})
	if err != nil {
		return nil, err
	}
	return UnlinkOIDCIdentity200JSONResponse(userSummaryToResponse(details)), nil
}

func userSummaryToResponse(u in.UserSummary) User {
	user := User{
		Id:            u.ID,
		Username:      u.Username,
		Role:          Role(u.Role),
		Status:        UserStatus(u.Status),
		PasswordLogin: u.PasswordLogin,
		OidcLinked:    u.OIDCLinked,
		CreatedAt:     u.CreatedAt,
	}
	if u.OIDCIdentity != nil {
		user.OidcIdentity = &OIDCIdentity{Issuer: u.OIDCIdentity.Issuer, Subject: u.OIDCIdentity.Subject}
	}
	return user
}

// newSessionToken returns 32 random bytes, base64url (spec §4.3). The
// adapter generates it so the command stays retryable (ddd-rules A-3).
func newSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// sessionCookie lives as long as the session's absolute lifetime; the
// idle timeout is enforced by the server. It is always Secure, HttpOnly
// and SameSite=Strict (spec §4.3); browsers accept Secure cookies on
// http://localhost, so local development still works.
func (s httpServer) sessionCookie(token string, expires time.Time) string {
	cookie := http.Cookie{
		Name:     server.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	return cookie.String()
}

func (s httpServer) clearedSessionCookie() string {
	cookie := http.Cookie{
		Name:     server.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	return cookie.String()
}
