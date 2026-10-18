package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// CachedAvailability asks the provider which fingerprints are cached.
// Fingerprints other than torrent hashes are reported as not cached.
type CachedAvailability struct {
	Actor        Actor
	ProviderID   string
	Fingerprints []string
}

// CachedAvailabilityResult maps each requested fingerprint to its cached flag.
type CachedAvailabilityResult struct {
	Cached map[string]bool
}

type CachedAvailabilityHandler decorator.QueryHandler[CachedAvailability, CachedAvailabilityResult]
