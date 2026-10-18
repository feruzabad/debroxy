package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// LogInWithOIDC starts a session for the user linked to a verified OIDC
// identity (DEC-04). The driving adapter completes the authorization code
// flow with PKCE and passes the verified issuer and subject, plus the
// session ID and token it generated (ddd-rules A-3).
type LogInWithOIDC struct {
	Issuer       string
	Subject      string
	SessionID    string
	SessionToken string
}

type LogInWithOIDCHandler decorator.CommandHandler[LogInWithOIDC]
