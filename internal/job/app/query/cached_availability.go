package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type cachedAvailabilityHandler struct {
	provider out.ProviderGateway
}

func NewCachedAvailabilityHandler(provider out.ProviderGateway, logger *slog.Logger, metricsClient decorator.MetricsClient) in.CachedAvailabilityHandler {
	if provider == nil {
		panic("nil providerGateway")
	}
	return decorator.ApplyQueryDecorators[in.CachedAvailability, in.CachedAvailabilityResult](
		cachedAvailabilityHandler{provider: provider}, logger, metricsClient)
}

// Handle passes torrent hashes through to the provider's cached check; any
// other fingerprint is not cached.
func (h cachedAvailabilityHandler) Handle(ctx context.Context, query in.CachedAvailability) (in.CachedAvailabilityResult, error) {
	if _, err := toDomainActor(query.Actor); err != nil {
		return in.CachedAvailabilityResult{}, err
	}
	provider, err := remoteitem.NewProviderID(query.ProviderID)
	if err != nil {
		return in.CachedAvailabilityResult{}, remoteitem.ProviderNotFoundError{Provider: query.ProviderID}
	}
	result := in.CachedAvailabilityResult{Cached: make(map[string]bool, len(query.Fingerprints))}
	hashOf := map[string]string{}
	var hashes []string
	for _, raw := range query.Fingerprints {
		result.Cached[raw] = false
		fingerprint, err := remoteitem.NewFingerprintFromString(raw)
		if err != nil {
			return in.CachedAvailabilityResult{}, remoteitem.InvalidSourceError{Reason: err.Error()}
		}
		if hash, ok := fingerprint.TorrentHash(); ok {
			hashOf[raw] = hash
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		return result, nil
	}
	cached, err := h.provider.CheckCached(ctx, provider, remoteitem.KindTorrent, hashes)
	if err != nil {
		return in.CachedAvailabilityResult{}, err
	}
	for raw, hash := range hashOf {
		result.Cached[raw] = cached[hash]
	}
	return result, nil
}
