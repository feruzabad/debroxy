package service_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/server"
	"github.com/feruzabad/debroxy/internal/identity/adapters/in/http/client"
	"github.com/feruzabad/debroxy/internal/identity/service"
)

const (
	origin        = "https://debroxy.example.org"
	adminPassword = "correct horse battery"
)

type harness struct {
	t      *testing.T
	server *httptest.Server
	client *client.ClientWithResponses
	clock  *clock.Fixed
}

// newHarness starts the real HTTP stack (the same middleware as cmd) on the
// component-test application, with a bootstrapped admin "root".
func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg := service.Config{SessionAbsoluteLifetime: 24 * time.Hour, SessionIdleTimeout: time.Hour}
	fixed := clock.NewFixed(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	logger := slog.New(slog.DiscardHandler)
	app, err := service.NewComponentTestApplication(cfg, fixed, logger)
	require.NoError(t, err)
	require.NoError(t, service.BootstrapAdmin(context.Background(), app, "root", strings.NewReader(adminPassword+"\n")))

	mux := http.NewServeMux()
	service.RegisterHTTPRoutes(mux, app, service.HTTPConfig{Clock: fixed}, logger)
	handler := server.Chain(mux,
		server.RequestID(),
		server.ClientIP(nil),
		server.Authenticate(server.AuthenticationConfig{AllowedOrigin: origin}, service.NewAuthenticator(app, logger), logger),
	)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	apiClient, err := client.NewClientWithResponses(httpServer.URL)
	require.NoError(t, err)
	return &harness{t: t, server: httpServer, client: apiClient, clock: fixed}
}

func bearer(token string) client.RequestEditorFn {
	return func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func (h *harness) logIn(username, password string) string {
	h.t.Helper()
	response, err := h.client.LogInWithResponse(context.Background(), client.LogInJSONRequestBody{Username: username, Password: password})
	require.NoError(h.t, err)
	require.Equal(h.t, http.StatusOK, response.StatusCode(), string(response.Body))
	return response.JSON200.Token
}

func (h *harness) createUser(adminToken, username, role string) client.User {
	h.t.Helper()
	response, err := h.client.CreateUserWithResponse(context.Background(),
		client.CreateUserJSONRequestBody{Username: username, Role: client.Role(role), Password: ptr("a fine passphrase")}, bearer(adminToken))
	require.NoError(h.t, err)
	require.Equal(h.t, http.StatusCreated, response.StatusCode(), string(response.Body))
	return *response.JSON201
}

// rootID finds the bootstrapped admin through the admin user list.
func (h *harness) rootID(adminToken string) string {
	h.t.Helper()
	users, err := h.client.ListUsersWithResponse(context.Background(), bearer(adminToken))
	require.NoError(h.t, err)
	require.Equal(h.t, http.StatusOK, users.StatusCode())
	for _, u := range users.JSON200.Users {
		if u.Username == "root" {
			return u.Id
		}
	}
	h.t.Fatal("no root user")
	return ""
}

func slugOf(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct{ Slug string }
	require.NoError(t, json.Unmarshal(body, &decoded), string(body))
	return decoded.Slug
}

func TestIdentity_login_me_logout(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	login, err := h.client.LogInWithResponse(ctx, client.LogInJSONRequestBody{Username: "root", Password: adminPassword})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, login.StatusCode())
	token := login.JSON200.Token
	assert.Len(t, token, 43)
	assert.Equal(t, time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC), login.JSON200.ExpiresAt.UTC(), "idle timeout from now")
	cookie := login.HTTPResponse.Header.Get("Set-Cookie")
	for _, attribute := range []string{"debroxy_session=" + token, "HttpOnly", "Secure", "SameSite=Strict", "Path=/"} {
		assert.Contains(t, cookie, attribute)
	}

	rootID := h.rootID(token)
	me, err := h.client.GetUserWithResponse(ctx, rootID, bearer(token))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, me.StatusCode())
	assert.Equal(t, "root", me.JSON200.Username)
	assert.Equal(t, client.RoleAdmin, me.JSON200.Role)
	assert.True(t, me.JSON200.PasswordLogin)

	logout, err := h.client.LogOutWithResponse(ctx, bearer(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, logout.StatusCode())
	assert.Contains(t, logout.HTTPResponse.Header.Get("Set-Cookie"), "Max-Age=0")

	after, err := h.client.GetUserWithResponse(ctx, rootID, bearer(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, after.StatusCode())
	assert.Equal(t, "session-expired", slugOf(t, after.Body))
}

func TestIdentity_session_expires_after_idle_timeout_unless_used(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	token := h.logIn("root", adminPassword)
	rootID := h.rootID(token)

	h.clock.Advance(50 * time.Minute)
	used, err := h.client.GetUserWithResponse(ctx, rootID, bearer(token))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, used.StatusCode(), "use extends the idle timeout")

	h.clock.Advance(50 * time.Minute)
	stillValid, err := h.client.GetUserWithResponse(ctx, rootID, bearer(token))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, stillValid.StatusCode())

	h.clock.Advance(61 * time.Minute)
	expired, err := h.client.GetUserWithResponse(ctx, rootID, bearer(token))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, expired.StatusCode())
}

func TestIdentity_protected_routes_reject_anonymous_calls(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	userID := "01927f6e-0000-7000-8000-000000000001"

	calls := map[string]func() (int, error){
		"logout": func() (int, error) {
			r, err := h.client.LogOutWithResponse(ctx)
			return statusCode(r, err)
		},
		"change_own_password": func() (int, error) {
			r, err := h.client.ChangeOwnPasswordWithResponse(ctx, client.ChangeOwnPasswordJSONRequestBody{CurrentPassword: "x", NewPassword: "y"})
			return statusCode(r, err)
		},
		"list_users": func() (int, error) {
			r, err := h.client.ListUsersWithResponse(ctx)
			return statusCode(r, err)
		},
		"create_user": func() (int, error) {
			r, err := h.client.CreateUserWithResponse(ctx, client.CreateUserJSONRequestBody{Username: "x", Role: client.RoleUser, Password: ptr("y")})
			return statusCode(r, err)
		},
		"get_user": func() (int, error) {
			r, err := h.client.GetUserWithResponse(ctx, userID)
			return statusCode(r, err)
		},
		"update_user": func() (int, error) {
			status := client.Disabled
			r, err := h.client.UpdateUserWithResponse(ctx, userID, client.UpdateUserJSONRequestBody{Status: &status})
			return statusCode(r, err)
		},
		"reset_user_password": func() (int, error) {
			r, err := h.client.ResetUserPasswordWithResponse(ctx, userID, client.ResetUserPasswordJSONRequestBody{Password: "y"})
			return statusCode(r, err)
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			status, err := call()
			require.NoError(t, err)
			assert.Equal(t, http.StatusUnauthorized, status)
		})
	}
}

func statusCode[R interface{ StatusCode() int }](response R, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	return response.StatusCode(), nil
}

func TestIdentity_failed_logins_are_uniform_then_throttled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	for i := range 5 {
		response, err := h.client.LogInWithResponse(ctx, client.LogInJSONRequestBody{Username: "root", Password: "wrong password " + string(rune('a'+i))})
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode())
		assert.Equal(t, "invalid-credentials", slugOf(t, response.Body))
	}
	unknown, err := h.client.LogInWithResponse(ctx, client.LogInJSONRequestBody{Username: "nobody", Password: "whatever value"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, unknown.StatusCode(), "the client IP is throttled too")

	throttled, err := h.client.LogInWithResponse(ctx, client.LogInJSONRequestBody{Username: "root", Password: adminPassword})
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, throttled.StatusCode())
	assert.Equal(t, "login-throttled", slugOf(t, throttled.Body))
	require.NotNil(t, throttled.JSON429)
	assert.Equal(t, 1, *throttled.JSON429.RetryAfter)
	assert.Equal(t, "1", throttled.HTTPResponse.Header.Get("Retry-After"))

	h.clock.Advance(16 * time.Minute)
	h.logIn("root", adminPassword)
}

func TestIdentity_expired_cookie_does_not_block_login(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	staleCookie := func(_ context.Context, r *http.Request) error {
		r.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: "expired-session-token-expired-session-token"})
		r.Header.Set("Origin", origin)
		return nil
	}

	response, err := h.client.LogInWithResponse(ctx, client.LogInJSONRequestBody{Username: "root", Password: adminPassword}, staleCookie)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode(), string(response.Body))
}

func TestIdentity_admin_manages_users(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)

	bob := h.createUser(admin, "Bob", "user")
	assert.Equal(t, "bob", bob.Username)
	bobToken := h.logIn("bob", "a fine passphrase")

	duplicate, err := h.client.CreateUserWithResponse(ctx,
		client.CreateUserJSONRequestBody{Username: "bob", Role: client.RoleUser, Password: ptr("a fine passphrase")}, bearer(admin))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, duplicate.StatusCode())
	assert.Equal(t, "username-taken", slugOf(t, duplicate.Body))

	forbidden, err := h.client.ListUsersWithResponse(ctx, bearer(bobToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, forbidden.StatusCode())

	disabled := client.Disabled
	update, err := h.client.UpdateUserWithResponse(ctx, bob.Id, client.UpdateUserJSONRequestBody{Status: &disabled}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, update.StatusCode(), string(update.Body))
	assert.Equal(t, client.Disabled, update.JSON200.Status)

	revoked, err := h.client.GetUserWithResponse(ctx, bob.Id, bearer(bobToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, revoked.StatusCode(), "disabling revokes sessions")

	users, err := h.client.ListUsersWithResponse(ctx, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, users.StatusCode())
	assert.Len(t, users.JSON200.Users, 2)
}

func TestIdentity_last_admin_is_protected(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)
	demote := client.RoleUser
	response, err := h.client.UpdateUserWithResponse(ctx, h.rootID(admin), client.UpdateUserJSONRequestBody{Role: &demote}, bearer(admin))

	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, response.StatusCode())
	assert.Equal(t, "last-admin", slugOf(t, response.Body))
}

func TestIdentity_admin_views_show_the_linked_oidc_identity(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)

	linked, err := h.client.CreateUserWithResponse(ctx, client.CreateUserJSONRequestBody{
		Username: "carol", Role: client.RoleUser,
		OidcIdentity: &client.OIDCIdentity{Issuer: "https://accounts.example.org", Subject: "carol-sub"},
	}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, linked.StatusCode(), string(linked.Body))
	require.NotNil(t, linked.JSON201.OidcIdentity)
	assert.Equal(t, "https://accounts.example.org", linked.JSON201.OidcIdentity.Issuer)
	assert.Equal(t, "carol-sub", linked.JSON201.OidcIdentity.Subject)

	passwordOnly := h.createUser(admin, "Bob", "user")
	assert.Nil(t, passwordOnly.OidcIdentity)

	got, err := h.client.GetUserWithResponse(ctx, linked.JSON201.Id, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	require.NotNil(t, got.JSON200.OidcIdentity)
	assert.Equal(t, "carol-sub", got.JSON200.OidcIdentity.Subject)

	list, err := h.client.ListUsersWithResponse(ctx, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode())
	byUsername := map[string]client.User{}
	for _, u := range list.JSON200.Users {
		byUsername[u.Username] = u
	}
	require.NotNil(t, byUsername["carol"].OidcIdentity)
	assert.Nil(t, byUsername["bob"].OidcIdentity)

	linkedBob, err := h.client.LinkOIDCIdentityWithResponse(ctx, passwordOnly.Id,
		client.LinkOIDCIdentityJSONRequestBody{Issuer: "https://accounts.example.org", Subject: "bob-sub"}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, linkedBob.StatusCode(), string(linkedBob.Body))
	self, err := h.client.GetUserWithResponse(ctx, passwordOnly.Id, bearer(h.logIn("bob", "a fine passphrase")))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, self.StatusCode(), string(self.Body))
	assert.True(t, self.JSON200.OidcLinked)
	assert.Nil(t, self.JSON200.OidcIdentity, "a non-admin's own view omits the identity")
}

func TestIdentity_change_own_password_keeps_only_the_current_session(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	current := h.logIn("root", adminPassword)
	other := h.logIn("root", adminPassword)
	rootID := h.rootID(current)

	response, err := h.client.ChangeOwnPasswordWithResponse(ctx,
		client.ChangeOwnPasswordJSONRequestBody{CurrentPassword: adminPassword, NewPassword: "a brand new passphrase"}, bearer(current))
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode(), string(response.Body))

	currentMe, err := h.client.GetUserWithResponse(ctx, rootID, bearer(current))
	require.NoError(t, err)
	otherMe, err := h.client.GetUserWithResponse(ctx, rootID, bearer(other))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, currentMe.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, otherMe.StatusCode())
	h.logIn("root", "a brand new passphrase")
}

func TestIdentity_admin_password_reset_revokes_sessions(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)
	carol := h.createUser(admin, "carol", "user")
	carolToken := h.logIn("carol", "a fine passphrase")

	reset, err := h.client.ResetUserPasswordWithResponse(ctx, carol.Id,
		client.ResetUserPasswordJSONRequestBody{Password: "reset by the admin"}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, reset.StatusCode(), string(reset.Body))

	revoked, err := h.client.GetUserWithResponse(ctx, carol.Id, bearer(carolToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, revoked.StatusCode())
	h.logIn("carol", "reset by the admin")
}

func ptr[T any](v T) *T { return &v }
