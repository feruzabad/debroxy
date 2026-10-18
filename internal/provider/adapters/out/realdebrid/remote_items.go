package realdebrid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// Endpoints (spec §9.2).
const (
	pathAddMagnet   = "/torrents/addMagnet"
	pathAddTorrent  = "/torrents/addTorrent"
	pathSelectFiles = "/torrents/selectFiles/"
	pathInfo        = "/torrents/info/"
	pathTorrents    = "/torrents"
	pathDelete      = "/torrents/delete/"
	pathHosts       = "/hosts/domains"
)

// defaultPageSize of list walks.
const defaultPageSize = 100

type addAnswer struct {
	ID string `json:"id"`
}

// CreateRemoteItem adds a torrent (never retried) and selects all files at
// once (DEC-15). Real-Debrid has no provider dedup (RD-N01): every add is
// a new item, so `cached` has no effect.
func (g *Gateway) CreateRemoteItem(ctx context.Context, source in.Source, _ bool) (in.CreateRemoteItemResult, error) {
	var request apiRequest
	switch source.Type {
	case in.SourceMagnet:
		request = apiRequest{
			method: http.MethodPost, path: pathAddMagnet,
			body: []byte(url.Values{"magnet": {source.Magnet}}.Encode()), contentType: "application/x-www-form-urlencoded",
		}
	case in.SourceTorrentFile:
		request = apiRequest{method: http.MethodPut, path: pathAddTorrent, body: source.File, contentType: "application/x-bittorrent"}
	default:
		return in.CreateRemoteItemResult{}, in.ProviderRejectedError{Provider: g.accountID, Detail: fmt.Sprintf("source type %s is not supported by this adapter", source.Type)}
	}
	response, err := g.send(ctx, request)
	if err != nil {
		return in.CreateRemoteItemResult{}, g.createError(nil, err)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return in.CreateRemoteItemResult{}, g.createError(&response, nil)
	}
	var added addAnswer
	if err := json.Unmarshal(response.body, &added); err != nil || added.ID == "" {
		return in.CreateRemoteItemResult{}, in.AmbiguousCreateError{Provider: g.accountID, Cause: "unreadable add answer"}
	}
	if err := g.selectAfterAdd(ctx, added.ID); err != nil {
		// The item exists; the next refresh selects again (see readInfo).
		g.logger.Warn("selecting files after add failed; the next refresh retries", "error", err.Error())
	}
	observation, err := g.DescribeRemoteItem(ctx, in.KindTorrent, added.ID)
	if err != nil {
		g.logger.Warn("look-up after add failed; reporting the item as queued", "error", err.Error())
		observation = in.Observation{ProviderItemID: added.ID, Kind: in.KindTorrent, State: in.StateQueued}
	}
	return in.CreateRemoteItemResult{ProviderItemID: added.ID, ProviderHash: observation.ProviderHash, Observation: observation}, nil
}

// selectAfterAdd reads the new item and selects its files once they are
// known; a magnet still converting has none yet and is selected by a later
// refresh (readInfo).
func (g *Gateway) selectAfterAdd(ctx context.Context, id string) error {
	info, err := g.fetchInfo(ctx, id)
	if err != nil {
		return err
	}
	if g.mapState(info.Status) != in.StateAwaitingFileSelection {
		return nil
	}
	return g.selectAllFiles(ctx, id, info.Files)
}

// selectAllFiles selects every file by ID. "files=all" is not used: for
// some torrents (a split RAR release) Real-Debrid answers 204 and selects
// nothing, and the item then shows downloading forever (RD-N09).
func (g *Gateway) selectAllFiles(ctx context.Context, id string, files []torrentFile) error {
	if len(files) == 0 {
		return nil
	}
	ids := make([]string, len(files))
	for i, file := range files {
		ids[i] = strconv.FormatInt(file.ID, 10)
	}
	response, err := g.send(ctx, apiRequest{
		method: http.MethodPost, path: pathSelectFiles + id,
		body: []byte(url.Values{"files": {strings.Join(ids, ",")}}.Encode()), contentType: "application/x-www-form-urlencoded", idempotent: true,
	})
	if err != nil {
		return g.transportError(err)
	}
	return g.check(response)
}

// DescribeRemoteItem reads /torrents/info; unknown_ressource means gone.
func (g *Gateway) DescribeRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) (in.Observation, error) {
	if kind != in.KindTorrent {
		return in.Observation{}, fmt.Errorf("describing %s items is not supported by this adapter", kind)
	}
	return g.readInfo(ctx, providerItemID)
}

// fetchInfo reads the raw /torrents/info answer.
func (g *Gateway) fetchInfo(ctx context.Context, id string) (torrentInfo, error) {
	response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: pathInfo + id, idempotent: true})
	if err != nil {
		return torrentInfo{}, g.transportError(err)
	}
	if err := g.check(response); err != nil {
		return torrentInfo{}, err
	}
	var info torrentInfo
	if err := json.Unmarshal(response.body, &info); err != nil {
		g.logOnce("shape:info", "unexpected Real-Debrid info answer", "error", err.Error())
		return torrentInfo{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected info answer"}
	}
	return info, nil
}

// readInfo reads one item. An item still waiting for file selection (an
// add whose selection failed) gets its files selected now; it reports
// awaiting_file_selection until the next refresh shows the result.
func (g *Gateway) readInfo(ctx context.Context, id string) (in.Observation, error) {
	info, err := g.fetchInfo(ctx, id)
	if err != nil {
		return in.Observation{}, err
	}
	observation := g.observation(info)
	if observation.State == in.StateAwaitingFileSelection {
		if err := g.selectAllFiles(ctx, id, info.Files); err != nil {
			g.logger.Warn("selecting files failed; the next refresh retries", "error", err.Error())
		}
	}
	if isFinished(observation.State) {
		g.finishedMu.Lock()
		g.finished[id] = observation
		g.finishedMu.Unlock()
	}
	return observation, nil
}

// ListRemoteItems walks /torrents for IDs and reads info for every item not
// already known as finished: the list status is stale and has no files
// (spec §5.6, RD-N04). Any failure makes the listing incomplete.
func (g *Gateway) ListRemoteItems(ctx context.Context, kinds []in.Kind) (in.RemoteItemListing, error) {
	listing := in.RemoteItemListing{Complete: true}
	listsTorrents := false
	for _, kind := range kinds {
		if kind == in.KindTorrent {
			listsTorrents = true
		} else {
			listing.Complete = false // no other kind is enabled for Real-Debrid
		}
	}
	if !listsTorrents {
		return listing, nil
	}
	items, err := g.listAll(ctx)
	if err != nil {
		return in.RemoteItemListing{}, err
	}
	present := map[string]bool{}
	for _, item := range items {
		present[item.ID] = true
		g.finishedMu.Lock()
		cached, known := g.finished[item.ID]
		g.finishedMu.Unlock()
		if known {
			listing.Observations = append(listing.Observations, cached)
			continue
		}
		observation, err := g.readInfo(ctx, item.ID)
		if err != nil {
			listing.Complete = false
			continue
		}
		listing.Observations = append(listing.Observations, observation)
	}
	g.forgetAbsent(present)
	return listing, nil
}

// listAll pages through /torrents until a short page or 204 (past the end).
func (g *Gateway) listAll(ctx context.Context) ([]listItem, error) {
	var all []listItem
	for page := 1; ; page++ {
		query := url.Values{"limit": {strconv.Itoa(g.pageSize)}, "page": {strconv.Itoa(page)}}
		response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: pathTorrents, query: query, idempotent: true})
		if err != nil {
			return nil, g.transportError(err)
		}
		if response.status == http.StatusNoContent {
			return all, nil
		}
		if err := g.check(response); err != nil {
			return nil, err
		}
		var items []listItem
		if err := json.Unmarshal(response.body, &items); err != nil {
			return nil, in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected list answer"}
		}
		all = append(all, items...)
		if len(items) < g.pageSize {
			return all, nil
		}
	}
}

func (g *Gateway) forgetAbsent(present map[string]bool) {
	g.finishedMu.Lock()
	defer g.finishedMu.Unlock()
	for id := range g.finished {
		if !present[id] {
			delete(g.finished, id)
		}
	}
}

// DeleteRemoteItem deletes; any answer other than 204 is an error and the
// operation verifies with a look-up (ADR 0003 rule 5).
func (g *Gateway) DeleteRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) error {
	if kind != in.KindTorrent {
		return fmt.Errorf("deleting %s items is not supported by this adapter", kind)
	}
	response, err := g.send(ctx, apiRequest{method: http.MethodDelete, path: pathDelete + providerItemID, idempotent: true})
	if err != nil {
		return g.transportError(err)
	}
	if err := g.check(response); err != nil {
		return err
	}
	g.finishedMu.Lock()
	delete(g.finished, providerItemID)
	g.finishedMu.Unlock()
	g.links.DropPrefix(linkCacheKey(providerItemID))
	return nil
}

// CheckCached: Real-Debrid disabled its availability endpoint (realdebrid.md
// R08); the capability table does not list it, so operations never call this.
func (g *Gateway) CheckCached(context.Context, in.Kind, []string) (in.CheckCachedResult, error) {
	return in.CheckCachedResult{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "Real-Debrid offers no cached check"}
}

// HosterCatalogue lists the supported hoster domains. Web links are not
// enabled for Real-Debrid in v1 (DEC-16), so operations do not call it yet.
func (g *Gateway) HosterCatalogue(ctx context.Context) (in.HosterCatalogue, error) {
	response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: pathHosts, idempotent: true})
	if err != nil {
		return in.HosterCatalogue{}, g.transportError(err)
	}
	if err := g.check(response); err != nil {
		return in.HosterCatalogue{}, err
	}
	var domains []string
	if err := json.Unmarshal(response.body, &domains); err != nil {
		return in.HosterCatalogue{}, in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected hoster answer"}
	}
	catalogue := in.HosterCatalogue{Hosters: make([]in.Hoster, 0, len(domains))}
	for _, domain := range domains {
		catalogue.Hosters = append(catalogue.Hosters, in.Hoster{Name: domain, Domains: []string{domain}, Up: true})
	}
	return catalogue, nil
}

// TranslateWebhook: Real-Debrid has no webhooks (spec §6.3).
func (g *Gateway) TranslateWebhook(map[string][]string, []byte) (in.WebhookSignal, error) {
	return in.WebhookSignal{}, in.ErrWebhookRejected
}
