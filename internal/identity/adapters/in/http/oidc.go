package http

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// OIDCConfig configures the OIDC login (spec §4.2, DEC-04).
type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// RedirectURL is <publicBaseURL>/api/v1/auth/oidc/callback.
	RedirectURL string
	// AfterLoginURL is where a logged-in browser goes, the public base URL.
	AfterLoginURL string
	// LoginErrorURL is where a failed callback goes, {publicBaseURL}/login?error=oidc (C14-02).
	LoginErrorURL string
	// Now is the clock for ID token checks.
	Now func() time.Time
}

const (
	oidcCookieName = "debroxy_oidc"
	oidcCookiePath = "/api/v1/auth/oidc"
	// oidcFlowLifetime bounds the time between the redirect and the callback.
	oidcFlowLifetime = 10 * time.Minute
)

var (
	errOIDCNotEnabled  = commonerrors.NewUnsupportedError("OIDC login is not enabled", "oidc-not-enabled")
	errOIDCUnavailable = commonerrors.NewProviderUnavailableError("the OIDC provider cannot be reached; try again later", "oidc-provider-unavailable")
	// errOIDCFailed is the uniform answer to every failed callback.
	errOIDCFailed = commonerrors.NewAuthenticationError("the OIDC login failed", "invalid-credentials")
)

// relyingParty runs the authorization code flow with PKCE (RFC 7636). The
// provider is discovered on first use, so a provider outage does not stop
// startup, and discovery is retried until it succeeds once.
type relyingParty struct {
	cfg OIDCConfig

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func newRelyingParty(cfg OIDCConfig) *relyingParty {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &relyingParty{cfg: cfg}
}

func (p *relyingParty) discover(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	if !p.cfg.Enabled {
		return nil, nil, errOIDCNotEnabled
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.oauth != nil {
		return p.oauth, p.verifier, nil
	}
	provider, err := oidc.NewProvider(ctx, p.cfg.IssuerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("discover OIDC provider: %w", errors.Join(errOIDCUnavailable, err))
	}
	p.oauth = &oauth2.Config{
		ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret, Endpoint: provider.Endpoint(),
		RedirectURL: p.cfg.RedirectURL, Scopes: p.cfg.Scopes,
	}
	p.verifier = provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID, Now: p.cfg.Now})
	return p.oauth, p.verifier, nil
}

// start returns the provider URL and the flow cookie holding state, nonce
// and PKCE verifier.
func (p *relyingParty) start(ctx context.Context) (redirect, cookie string, err error) {
	oauth, _, err := p.discover(ctx)
	if err != nil {
		return "", "", err
	}
	state, nonce, verifier := randomToken(), randomToken(), oauth2.GenerateVerifier()
	redirect = oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
	flow := http.Cookie{
		Name: oidcCookieName, Value: strings.Join([]string{state, nonce, verifier}, "."), Path: oidcCookiePath,
		MaxAge: int(oidcFlowLifetime.Seconds()), HttpOnly: true, Secure: true,
		// Lax: the provider's redirect back is a cross-site top-level GET.
		SameSite: http.SameSiteLaxMode,
	}
	return redirect, flow.String(), nil
}

// finish checks the state against the flow cookie, redeems the code with
// the PKCE verifier and verifies the ID token and its nonce. It returns
// the verified issuer and subject.
func (p *relyingParty) finish(ctx context.Context, code, state, flowCookie string) (issuer, subject string, err error) {
	oauth, verifier, err := p.discover(ctx)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(flowCookie, ".")
	if len(parts) != 3 || code == "" || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(state)) != 1 {
		return "", "", errors.Join(errOIDCFailed, errors.New("state mismatch or missing code"))
	}
	token, err := oauth.Exchange(ctx, code, oauth2.VerifierOption(parts[2]))
	if err != nil {
		return "", "", errors.Join(errOIDCFailed, fmt.Errorf("exchange code: %w", err))
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", "", errors.Join(errOIDCFailed, errors.New("no id_token in the token response"))
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", errors.Join(errOIDCFailed, fmt.Errorf("verify id_token: %w", err))
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(parts[1])) != 1 {
		return "", "", errors.Join(errOIDCFailed, errors.New("nonce mismatch"))
	}
	return idToken.Issuer, idToken.Subject, nil
}

func randomToken() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // crypto/rand.Read never fails
	return base64.RawURLEncoding.EncodeToString(raw)
}
