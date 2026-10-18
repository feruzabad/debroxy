package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ProviderHosters returns the provider's hoster catalogue (C9-03).
type ProviderHosters struct {
	Actor      Actor
	ProviderID string
}

// HosterView is one supported web hoster; the daily figures are nil when
// the provider reports none.
type HosterView struct {
	Name       string
	Domains    []string
	Up         bool
	DailyLimit *int64
	DailyUsed  *int64
}

type ProviderHostersHandler decorator.QueryHandler[ProviderHosters, []HosterView]
