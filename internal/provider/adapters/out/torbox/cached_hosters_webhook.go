package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

type cachedEntry struct {
	Hash  string     `json:"hash"`
	Files []itemFile `json:"files"`
}

// CheckCached asks the torrent cache index (torbox.md T09). Uncached hashes
// are absent from the answer. Web submits never check (ADR 0013 rule 14).
func (g *Gateway) CheckCached(ctx context.Context, kind in.Kind, providerHashes []string) (in.CheckCachedResult, error) {
	if kind != in.KindTorrent {
		return in.CheckCachedResult{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "cached checks are used for torrents only"}
	}
	query := url.Values{"hash": {strings.Join(providerHashes, ",")}, "format": {"object"}, "list_files": {"true"}}
	response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: pathTorrentCached, query: query, class: classGeneral, idempotent: true})
	if err != nil {
		return in.CheckCachedResult{}, g.transportError(err)
	}
	data, err := g.decode(response)
	if err != nil {
		return in.CheckCachedResult{}, err
	}
	var found map[string]cachedEntry
	// With nothing cached the data may be null, {} or [].
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &found); err != nil {
			return in.CheckCachedResult{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected cached-check answer"}
		}
	}
	byHash := map[string]cachedEntry{}
	for hash, entry := range found {
		byHash[strings.ToLower(hash)] = entry
	}
	result := in.CheckCachedResult{Items: make([]in.CachedItem, 0, len(providerHashes))}
	for _, hash := range providerHashes {
		entry, cached := byHash[strings.ToLower(hash)]
		result.Items = append(result.Items, in.CachedItem{ProviderHash: strings.ToLower(hash), Cached: cached, Files: files(entry.Files)})
	}
	return result, nil
}

type hoster struct {
	Name           string   `json:"name"`
	Domains        []string `json:"domains"`
	Status         bool     `json:"status"`
	DailyLinkLimit *int64   `json:"daily_link_limit"`
	DailyLinkUsed  *int64   `json:"daily_link_used"`
}

// HosterCatalogue returns the cached catalogue, refreshed every hour
// (ADR 0003 rule 10). A failed refresh keeps serving the previous one.
func (g *Gateway) HosterCatalogue(ctx context.Context) (in.HosterCatalogue, error) {
	g.hostersMu.Lock()
	defer g.hostersMu.Unlock()
	now := g.clock.Now()
	if !g.hostersLoadedAt.IsZero() && now.Sub(g.hostersLoadedAt) < hosterCatalogueMaxAge {
		return g.hosters, nil
	}
	var hosters []hoster
	if err := g.getData(ctx, pathHosters, url.Values{}, &hosters); err != nil {
		if !g.hostersLoadedAt.IsZero() {
			g.logger.Warn("hoster catalogue refresh failed; serving the previous catalogue", "error", err.Error())
			return g.hosters, nil
		}
		return in.HosterCatalogue{}, err
	}
	catalogue := in.HosterCatalogue{Hosters: make([]in.Hoster, 0, len(hosters))}
	for _, h := range hosters {
		catalogue.Hosters = append(catalogue.Hosters, in.Hoster{
			Name: h.Name, Domains: h.Domains, Up: h.Status, DailyLimit: h.DailyLinkLimit, DailyUsed: h.DailyLinkUsed,
		})
	}
	g.hosters, g.hostersLoadedAt = catalogue, now
	return catalogue, nil
}

// TranslateWebhook: TorBox webhooks carry no item identity (torbox.md T12),
// so any well-formed notification only asks for a refresh.
func (g *Gateway) TranslateWebhook(_ map[string][]string, body []byte) (in.WebhookSignal, error) {
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Type == "" {
		return in.WebhookSignal{}, in.ErrWebhookRejected
	}
	return in.WebhookSignal{RefreshRequested: true}, nil
}
