package realdebrid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Download history sweep (DEC-25, RD-N05): every unrestrict leaves an entry
// in /downloads. Once a day, entries older than 24 hours are deleted.
// Deleting an entry does not break a link in use (RD-N02).
const (
	historySweepInterval = 24 * time.Hour
	historyMaxAge        = 24 * time.Hour
	// timestampMargin covers Real-Debrid's local-time timestamps labelled
	// as UTC (RD-N03): no time zone is further than 14 hours from UTC.
	timestampMargin = 14 * time.Hour
	pathDownloads   = "/downloads"
	pathDeleteEntry = "/downloads/delete/"
)

type historyEntry struct {
	ID        string `json:"id"`
	Generated string `json:"generated"`
}

// SweepDownloadHistory deletes the account's old /downloads entries and
// returns how many it deleted.
func (g *Gateway) SweepDownloadHistory(ctx context.Context) (int, error) {
	cutoff := g.clock.Now().Add(-historyMaxAge - timestampMargin)
	var old []string
	for page := 1; ; page++ {
		query := url.Values{"limit": {strconv.Itoa(g.pageSize)}, "page": {strconv.Itoa(page)}}
		response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: pathDownloads, query: query, idempotent: true})
		if err != nil {
			return 0, g.transportError(err)
		}
		if response.status == http.StatusNoContent {
			break
		}
		if err := g.check(response); err != nil {
			return 0, err
		}
		var entries []historyEntry
		if err := json.Unmarshal(response.body, &entries); err != nil {
			return 0, err
		}
		for _, entry := range entries {
			generated, err := time.Parse(time.RFC3339Nano, entry.Generated)
			if err == nil && generated.Before(cutoff) {
				old = append(old, entry.ID)
			}
		}
		if len(entries) < g.pageSize {
			break
		}
	}
	deleted := 0
	for _, id := range old {
		response, err := g.send(ctx, apiRequest{method: http.MethodDelete, path: pathDeleteEntry + id, idempotent: true})
		if err != nil {
			return deleted, g.transportError(err)
		}
		if err := g.check(response); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// RunDownloadHistorySweep sweeps once a day until ctx ends. The first sweep
// runs after one interval, so restarts do not cause extra API calls.
func (g *Gateway) RunDownloadHistorySweep(ctx context.Context) {
	timer := time.NewTimer(historySweepInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			deleted, err := g.SweepDownloadHistory(ctx)
			if err != nil {
				g.logger.Warn("download history sweep failed", "error", err.Error())
			} else if deleted > 0 {
				g.logger.Info("download history swept", "deleted", deleted)
			}
			timer.Reset(historySweepInterval)
		}
	}
}
