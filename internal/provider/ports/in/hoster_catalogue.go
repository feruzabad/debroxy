package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// HosterCatalogueOperation returns the provider's supported web hosters (ADR 0003 rule 10).
type HosterCatalogueOperation decorator.OperationHandler[HosterCatalogueRequest, HosterCatalogue]

type HosterCatalogueRequest struct {
	ProviderID string
}

type HosterCatalogue struct {
	Hosters []Hoster
}

// Hoster is one supported web hoster.
type Hoster struct {
	Name    string
	Domains []string
	Up      bool
	// DailyLimit and DailyUsed are nil when the provider reports none.
	DailyLimit *int64
	DailyUsed  *int64
}
