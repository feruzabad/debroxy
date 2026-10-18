package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// LogInWithPassword starts a session. The driving adapter generates the
// session ID and token (ddd-rules A-3); ClientIP feeds login throttling.
type LogInWithPassword struct {
	Username     string
	Password     string
	ClientIP     string
	SessionID    string
	SessionToken string
}

type LogInWithPasswordHandler decorator.CommandHandler[LogInWithPassword]
