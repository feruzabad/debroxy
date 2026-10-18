package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// Endpoints (spec §9.1). Each is its own rate bucket (ADR 0013 rule 3).
const (
	pathCreateTorrent     = "/torrents/createtorrent"
	pathTorrentList       = "/torrents/mylist"
	pathControlTorrent    = "/torrents/controltorrent"
	pathTorrentCached     = "/torrents/checkcached"
	pathCreateWebDownload = "/webdl/createwebdownload"
	pathWebList           = "/webdl/mylist"
	pathControlWeb        = "/webdl/controlwebdownload"
	pathHosters           = "/webdl/hosters"
	pathQueued            = "/queued/getqueued"
)

type createTorrentData struct {
	Hash      string `json:"hash"`
	TorrentID int64  `json:"torrent_id"`
}

type createWebData struct {
	Hash          string `json:"hash"`
	WebDownloadID int64  `json:"webdownload_id"`
}

// CreateRemoteItem sends one create, never retried (NFR-09). A torrent
// create is charged to the hourly bucket unless CheckCached reported the
// hash cached; a web create always is (ADR 0013 rule 4).
func (g *Gateway) CreateRemoteItem(ctx context.Context, source in.Source, cached bool) (in.CreateRemoteItemResult, error) {
	fields := map[string]string{}
	var file []byte
	var path, class, alternative string
	var kind in.Kind
	switch source.Type {
	case in.SourceMagnet:
		path, kind, fields["magnet"], fields["allow_zip"] = pathCreateTorrent, in.KindTorrent, source.Magnet, "true"
	case in.SourceTorrentFile:
		path, kind, file, fields["allow_zip"] = pathCreateTorrent, in.KindTorrent, source.File, "true"
	case in.SourceWebURL:
		path, kind, fields["link"] = pathCreateWebDownload, in.KindWeb, source.URL
		if source.Password != "" {
			fields["password"] = source.Password // ASM-T26
		}
	default:
		return in.CreateRemoteItemResult{}, in.ProviderRejectedError{Provider: g.accountID, Detail: fmt.Sprintf("source type %s is not supported by this adapter", source.Type)}
	}
	class, alternative = classUncachedCreate, classGeneral
	if kind == in.KindTorrent && cached {
		class, alternative = classGeneral, classUncachedCreate
	}
	body, contentType, err := multipartBody(fields, source.FileName, file)
	if err != nil {
		return in.CreateRemoteItemResult{}, err
	}
	response, sendErr := g.send(ctx, apiRequest{
		method: http.MethodPost, path: path, body: body, contentType: contentType,
		class: class, alternatives: []string{alternative},
	})
	if sendErr != nil {
		return in.CreateRemoteItemResult{}, g.createError(nil, sendErr)
	}
	if response.status >= 300 {
		return in.CreateRemoteItemResult{}, g.createError(&response, nil)
	}
	data, err := g.decode(response)
	if err != nil {
		return in.CreateRemoteItemResult{}, g.createError(&response, nil)
	}

	var providerItemID int64
	var providerHash string
	if kind == in.KindTorrent {
		var created createTorrentData
		if err := json.Unmarshal(data, &created); err != nil || created.TorrentID == 0 {
			return in.CreateRemoteItemResult{}, in.AmbiguousCreateError{Provider: g.accountID, Cause: "unreadable create answer"}
		}
		providerItemID, providerHash = created.TorrentID, created.Hash
	} else {
		var created createWebData
		if err := json.Unmarshal(data, &created); err != nil || created.WebDownloadID == 0 {
			return in.CreateRemoteItemResult{}, in.AmbiguousCreateError{Provider: g.accountID, Cause: "unreadable create answer"}
		}
		providerItemID, providerHash = created.WebDownloadID, created.Hash
	}
	result := in.CreateRemoteItemResult{
		ProviderItemID: strconv.FormatInt(providerItemID, 10),
		ProviderHash:   lower(providerHash),
	}
	// The create answer has no state; one look-up gives the observation. If
	// it fails, the item is reported queued and the next refresh corrects it.
	observation, err := g.DescribeRemoteItem(ctx, kind, result.ProviderItemID)
	if err != nil {
		g.logger.Warn("look-up after create failed; reporting the item as queued", "error", err.Error())
		observation = in.Observation{ProviderItemID: result.ProviderItemID, Kind: kind, ProviderHash: result.ProviderHash, State: in.StateQueued}
	}
	result.Observation = observation
	return result, nil
}

// ListRemoteItems lists every item of the given kinds and the queue. The
// listing is Complete only if every list was read (spec §5.6 step 1).
func (g *Gateway) ListRemoteItems(ctx context.Context, kinds []in.Kind) (in.RemoteItemListing, error) {
	listing := in.RemoteItemListing{Complete: true}
	var lastErr error
	succeeded := 0
	add := func(err error) {
		if err != nil {
			listing.Complete, lastErr = false, err
			return
		}
		succeeded++
	}
	for _, kind := range kinds {
		switch kind {
		case in.KindTorrent:
			items, err := listPages[torrentItem](ctx, g, pathTorrentList)
			for _, item := range items {
				listing.Observations = append(listing.Observations, g.torrentObservation(item))
			}
			add(err)
		case in.KindWeb:
			items, err := listPages[webItem](ctx, g, pathWebList)
			for _, item := range items {
				listing.Observations = append(listing.Observations, g.webObservation(item))
			}
			add(err)
		default:
			add(fmt.Errorf("listing %s items is not supported by this adapter", kind))
		}
	}
	var queued []queuedItem
	err := g.getData(ctx, pathQueued, url.Values{"bypass_cache": {"true"}}, &queued)
	for _, item := range queued {
		listing.Observations = append(listing.Observations, queuedObservation(item))
	}
	add(err)
	if succeeded == 0 && lastErr != nil {
		return in.RemoteItemListing{}, lastErr
	}
	return listing, nil
}

// listPages walks a mylist with limit and offset until a short page:
// TorBox pages its lists, so one plain call could miss items.
func listPages[T any](ctx context.Context, g *Gateway, path string) ([]T, error) {
	var all []T
	for offset := 0; ; offset += g.pageSize {
		var page []T
		query := url.Values{"bypass_cache": {"true"}, "limit": {strconv.Itoa(g.pageSize)}, "offset": {strconv.Itoa(offset)}}
		if err := g.getData(ctx, path, query, &page); err != nil {
			return all, err
		}
		all = append(all, page...)
		if len(page) < g.pageSize {
			return all, nil
		}
	}
}

// DescribeRemoteItem reads one item by ID; ITEM_NOT_FOUND means gone.
func (g *Gateway) DescribeRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) (in.Observation, error) {
	query := url.Values{"id": {providerItemID}, "bypass_cache": {"true"}}
	switch kind {
	case in.KindTorrent:
		var item torrentItem
		if err := g.getData(ctx, pathTorrentList, query, &item); err != nil {
			return in.Observation{}, err
		}
		return g.torrentObservation(item), nil
	case in.KindWeb:
		var item webItem
		if err := g.getData(ctx, pathWebList, query, &item); err != nil {
			return in.Observation{}, err
		}
		return g.webObservation(item), nil
	}
	return in.Observation{}, fmt.Errorf("describing %s items is not supported by this adapter", kind)
}

// DeleteRemoteItem asks TorBox to delete; any answer other than success is
// an error (the operation verifies with a look-up, ADR 0003 rule 5).
func (g *Gateway) DeleteRemoteItem(ctx context.Context, kind in.Kind, providerItemID string) error {
	id, err := strconv.ParseInt(providerItemID, 10, 64)
	if err != nil {
		return in.ErrRemoteItemNotFound
	}
	var path, idField string
	switch kind {
	case in.KindTorrent:
		path, idField = pathControlTorrent, "torrent_id"
	case in.KindWeb:
		path, idField = pathControlWeb, "webdl_id"
	default:
		return fmt.Errorf("deleting %s items is not supported by this adapter", kind)
	}
	body, err := json.Marshal(map[string]any{idField: id, "operation": "delete"})
	if err != nil {
		return err
	}
	response, err := g.send(ctx, apiRequest{
		method: http.MethodPost, path: path, body: body, contentType: "application/json",
		class: classGeneral, idempotent: true,
	})
	if err != nil {
		return g.transportError(err)
	}
	if _, err := g.decode(response); err != nil {
		return err
	}
	g.links.DropPrefix(linkCacheKey(kind, providerItemID))
	return nil
}

// getData performs an idempotent GET and decodes the envelope's data into target.
func (g *Gateway) getData(ctx context.Context, path string, query url.Values, target any) error {
	response, err := g.send(ctx, apiRequest{method: http.MethodGet, path: path, query: query, class: classGeneral, idempotent: true})
	if err != nil {
		return g.transportError(err)
	}
	data, err := g.decode(response)
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		g.logOnce("shape:"+path, "unexpected TorBox answer shape", "endpoint", path, "error", err.Error())
		return in.ProviderRejectedError{Provider: g.accountID, Detail: "unexpected answer shape from " + path}
	}
	return nil
}

func multipartBody(fields map[string]string, fileName string, file []byte) ([]byte, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, name := range []string{"magnet", "link", "password", "allow_zip"} {
		if value, ok := fields[name]; ok {
			if err := writer.WriteField(name, value); err != nil {
				return nil, "", err
			}
		}
	}
	if file != nil {
		if fileName == "" {
			fileName = "upload.torrent"
		}
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(file); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func lower(s string) string { return string(bytes.ToLower([]byte(s))) }
