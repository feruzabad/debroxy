package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// Refresh causes (spec §5.6).
const (
	RefreshCauseRead     = "read"
	RefreshCauseForced   = "forced"
	RefreshCauseWebhook  = "webhook"
	RefreshCauseSafety   = "safety"
	RefreshCauseFollowUp = "follow-up"
	// RefreshCauseWatch is issued while Event Streams are open; it is paced
	// like read (ADR 0021 rules 8–10).
	RefreshCauseWatch = "watch"
)

// RefreshProviderState runs the refresh pipeline for one provider (spec
// §5.6). Actor is set for read and forced refreshes; forced refreshes are
// rate limited per user (refresh-rate-exceeded). When the refresh did not
// run or did not complete, read and forced refreshes answer
// remoteitem.ErrStateStale.
type RefreshProviderState struct {
	Actor      Actor
	ProviderID string
	Cause      string
}

type RefreshProviderStateHandler decorator.CommandHandler[RefreshProviderState]
