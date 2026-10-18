package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/server"
	"github.com/feruzabad/debroxy/internal/identity/adapters/in/http/client"
	"github.com/feruzabad/debroxy/internal/identity/service"
)

// fakeOIDC is a minimal OpenID provider: discovery, keys and a token
// endpoint that checks the PKCE verifier. The test plays the browser and
// the provider's login page by calling the callback itself.
type fakeOIDC struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	clock  *clock.Fixed

	mu    sync.Mutex
	codes map[string]grant // code → what the login granted
}

type grant struct {
	subject   string
	nonce     string
	challenge string
}

func newFakeOIDC(t *testing.T, clk *clock.Fixed) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeOIDC{t: t, key: key, clock: clk, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token",
			"jwks_uri": f.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", f.token)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// login is the provider's login page: it grants a code for the subject
// against the authorization request's nonce and PKCE challenge.
func (f *fakeOIDC) login(authorizeURL, subject string) (code, state string) {
	f.t.Helper()
	u, err := url.Parse(authorizeURL)
	require.NoError(f.t, err)
	q := u.Query()
	require.Equal(f.t, "S256", q.Get("code_challenge_method"))
	require.Equal(f.t, "debroxy", q.Get("client_id"))
	code = "code-" + subject
	f.mu.Lock()
	f.codes[code] = grant{subject: subject, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	f.mu.Unlock()
	return code, q.Get("state")
}

func (f *fakeOIDC) token(w http.ResponseWriter, r *http.Request) {
	require.NoError(f.t, r.ParseForm())
	f.mu.Lock()
	g, ok := f.codes[r.Form.Get("code")]
	delete(f.codes, r.Form.Get("code"))
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	require.NoError(f.t, err)
	now := f.clock.Now()
	claims, err := json.Marshal(map[string]any{
		"iss": f.server.URL, "sub": g.subject, "aud": "debroxy", "nonce": g.nonce,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})
	require.NoError(f.t, err)
	signed, err := signer.Sign(claims)
	require.NoError(f.t, err)
	idToken, err := signed.CompactSerialize()
	require.NoError(f.t, err)
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": idToken, "expires_in": 300})
}

// newOIDCHarness starts the identity API with OIDC enabled against fake.
func newOIDCHarness(t *testing.T) (*harness, *fakeOIDC) {
	t.Helper()
	fixed := clock.NewFixed(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	fake := newFakeOIDC(t, fixed)
	logger := slog.New(slog.DiscardHandler)
	app, err := service.NewComponentTestApplication(service.Config{SessionAbsoluteLifetime: 24 * time.Hour, SessionIdleTimeout: time.Hour}, fixed, logger)
	require.NoError(t, err)
	require.NoError(t, service.BootstrapAdmin(context.Background(), app, "root", strings.NewReader(adminPassword+"\n")))

	mux := http.NewServeMux()
	service.RegisterHTTPRoutes(mux, app, service.HTTPConfig{Clock: fixed, OIDC: service.OIDCConfig{
		Enabled: true, IssuerURL: fake.server.URL, ClientID: "debroxy", ClientSecret: "client-secret",
		Scopes: []string{"openid"}, PublicBaseURL: origin,
	}}, logger)
	handler := server.Chain(mux, server.RequestID(), server.ClientIP(nil),
		server.Authenticate(server.AuthenticationConfig{AllowedOrigin: origin}, service.NewAuthenticator(app, logger), logger))
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	apiClient, err := client.NewClientWithResponses(httpServer.URL)
	require.NoError(t, err)
	return &harness{t: t, server: httpServer, client: apiClient, clock: fixed}, fake
}

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// startLogin follows the debroxy side of the redirect and returns the
// provider URL and the flow cookie.
func (h *harness) startLogin() (authorizeURL string, flowCookie *http.Cookie) {
	h.t.Helper()
	response, err := noRedirects.Get(h.server.URL + "/api/v1/auth/oidc/login")
	require.NoError(h.t, err)
	require.NoError(h.t, response.Body.Close())
	require.Equal(h.t, http.StatusFound, response.StatusCode)
	cookies := response.Cookies()
	require.Len(h.t, cookies, 1)
	assert.True(h.t, cookies[0].HttpOnly)
	assert.Equal(h.t, http.SameSiteLaxMode, cookies[0].SameSite)
	return response.Header.Get("Location"), cookies[0]
}

// callbackAnswer is what a test needs of the callback's response.
type callbackAnswer struct {
	StatusCode int
	Location   string
	Cookies    []*http.Cookie
}

func (h *harness) callback(code, state string, flowCookie *http.Cookie) callbackAnswer {
	h.t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		h.server.URL+"/api/v1/auth/oidc/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), nil)
	require.NoError(h.t, err)
	if flowCookie != nil {
		request.AddCookie(flowCookie)
	}
	response, err := noRedirects.Do(request)
	require.NoError(h.t, err)
	require.NoError(h.t, response.Body.Close())
	return callbackAnswer{StatusCode: response.StatusCode, Location: response.Header.Get("Location"), Cookies: response.Cookies()}
}

func TestOIDC_logs_in_a_linked_user(t *testing.T) {
	t.Parallel()
	h, fake := newOIDCHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)
	alice := h.createUser(admin, "alice", "user")
	linked, err := h.client.LinkOIDCIdentityWithResponse(ctx, alice.Id, client.LinkOIDCIdentityJSONRequestBody{Issuer: fake.server.URL, Subject: "alice-sub"}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, linked.StatusCode(), string(linked.Body))
	assert.True(t, linked.JSON200.OidcLinked)

	authorizeURL, flow := h.startLogin()
	assert.True(t, strings.HasPrefix(authorizeURL, fake.server.URL+"/authorize?"), authorizeURL)
	code, state := fake.login(authorizeURL, "alice-sub")
	response := h.callback(code, state, flow)

	require.Equal(t, http.StatusFound, response.StatusCode)
	assert.Equal(t, origin+"/", response.Location)
	var session *http.Cookie
	for _, c := range response.Cookies {
		if c.Name == server.SessionCookieName {
			session = c
		}
	}
	require.NotNil(t, session)
	me, err := h.client.GetUserWithResponse(ctx, alice.Id, bearer(session.Value))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, me.StatusCode(), "the OIDC session works like a password session")
}

func TestOIDC_refuses_unlinked_users_and_forged_callbacks(t *testing.T) {
	t.Parallel()
	h, fake := newOIDCHarness(t)

	assertFailed := func(answer callbackAnswer) {
		h.t.Helper()
		assert.Equal(h.t, http.StatusFound, answer.StatusCode, "a failed callback redirects, not 401 (C14-02)")
		assert.Equal(h.t, origin+"/login?error=oidc", answer.Location, "the cause never leaks into the URL")
		cleared := false
		for _, c := range answer.Cookies {
			if c.Name == "debroxy_oidc" && c.MaxAge <= 0 {
				cleared = true
			}
		}
		assert.True(h.t, cleared, "the flow cookie is cleared")
	}

	authorizeURL, flow := h.startLogin()
	code, state := fake.login(authorizeURL, "stranger")
	assertFailed(h.callback(code, state, flow))

	authorizeURL, flow = h.startLogin()
	code, _ = fake.login(authorizeURL, "stranger")
	assertFailed(h.callback(code, "forged-state", flow))

	authorizeURL, _ = h.startLogin()
	code, state = fake.login(authorizeURL, "stranger")
	assertFailed(h.callback(code, state, nil))

	_, flow = h.startLogin()
	assertFailed(h.callback("unknown-code", strings.Split(flow.Value, ".")[0], flow))
}

func TestOIDC_provider_error_redirects(t *testing.T) {
	t.Parallel()
	h, _ := newOIDCHarness(t)

	response, err := noRedirects.Get(h.server.URL + "/api/v1/auth/oidc/callback?error=access_denied")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusFound, response.StatusCode)
	assert.Equal(t, origin+"/login?error=oidc", response.Header.Get("Location"))
}

func TestOIDC_callback_redirects_when_the_provider_is_unreachable(t *testing.T) {
	t.Parallel()
	fixed := clock.NewFixed(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	logger := slog.New(slog.DiscardHandler)
	app, err := service.NewComponentTestApplication(service.Config{SessionAbsoluteLifetime: 24 * time.Hour, SessionIdleTimeout: time.Hour}, fixed, logger)
	require.NoError(t, err)
	require.NoError(t, service.BootstrapAdmin(context.Background(), app, "root", strings.NewReader(adminPassword+"\n")))

	mux := http.NewServeMux()
	service.RegisterHTTPRoutes(mux, app, service.HTTPConfig{Clock: fixed, OIDC: service.OIDCConfig{
		Enabled: true, IssuerURL: "http://127.0.0.1:1", ClientID: "debroxy", ClientSecret: "client-secret",
		Scopes: []string{"openid"}, PublicBaseURL: origin,
	}}, logger)
	handler := server.Chain(mux, server.RequestID(), server.ClientIP(nil),
		server.Authenticate(server.AuthenticationConfig{AllowedOrigin: origin}, service.NewAuthenticator(app, logger), logger))
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	response, err := noRedirects.Get(httpServer.URL + "/api/v1/auth/oidc/callback?code=x&state=y")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusFound, response.StatusCode)
	assert.Equal(t, origin+"/login?error=oidc", response.Header.Get("Location"))
}

func TestOIDC_disabled_answers_not_enabled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	response, err := noRedirects.Get(h.server.URL + "/api/v1/auth/oidc/login")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusNotImplemented, response.StatusCode)
}

func TestOIDC_admin_unlinks_but_keeps_the_only_login_method(t *testing.T) {
	t.Parallel()
	h, fake := newOIDCHarness(t)
	ctx := context.Background()
	admin := h.logIn("root", adminPassword)
	created, err := h.client.CreateUserWithResponse(ctx, client.CreateUserJSONRequestBody{
		Username: "carol", Role: client.RoleUser, OidcIdentity: &client.OIDCIdentity{Issuer: fake.server.URL, Subject: "carol-sub"},
	}, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	assert.False(t, created.JSON201.PasswordLogin)

	unlink, err := h.client.UnlinkOIDCIdentityWithResponse(ctx, created.JSON201.Id, bearer(admin))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, unlink.StatusCode())
	assert.Equal(t, "no-login-method", slugOf(t, unlink.Body))
}
