package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	debianHash  = "7acf8fb590b2060dd9c3146ef770169d593433b0"
	debianID    = 116409037
	isoContent  = "0123456789abcdefghijklmnopqrstuvwxyz"
	adminSecret = "correct horse battery staple"
)

// fakeTorBox stands in for api.torbox.app and its CDN over TLS. It keeps
// one account's items and answers in TorBox's envelope (torbox.md §2).
type fakeTorBox struct {
	server  *httptest.Server
	mu      sync.Mutex
	items   map[int64]bool
	deleted []int64
}

func newFakeTorBox(t *testing.T) *fakeTorBox {
	t.Helper()
	f := &fakeTorBox{items: map[int64]bool{}}
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "error": nil, "detail": "ok", "data": data})
	}
	item := func() map[string]any {
		return map[string]any{
			"id": debianID, "hash": debianHash, "name": "debian-netinst", "size": len(isoContent), "download_state": "cached",
			"progress": 1, "download_present": true, "download_finished": true, "eta": 0,
			"files": []map[string]any{{"id": 0, "name": "debian-netinst/debian.iso", "size": len(isoContent)}},
		}
	}
	mux.HandleFunc("GET /v1/api/torrents/checkcached", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{r.URL.Query().Get("hash"): map[string]any{"name": "debian-netinst", "size": len(isoContent), "hash": debianHash}})
	})
	mux.HandleFunc("POST /v1/api/torrents/createtorrent", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.items[debianID] = true
		f.mu.Unlock()
		ok(w, map[string]any{"hash": debianHash, "torrent_id": debianID, "auth_id": "x"})
	})
	mux.HandleFunc("GET /v1/api/torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		present := f.items[debianID]
		f.mu.Unlock()
		if r.URL.Query().Get("id") != "" {
			if !present {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "ITEM_NOT_FOUND", "detail": "not found", "data": nil})
				return
			}
			ok(w, item())
			return
		}
		list := []any{}
		if present {
			list = append(list, item())
		}
		ok(w, list)
	})
	mux.HandleFunc("GET /v1/api/webdl/mylist", func(w http.ResponseWriter, _ *http.Request) { ok(w, []any{}) })
	mux.HandleFunc("GET /v1/api/queued/getqueued", func(w http.ResponseWriter, _ *http.Request) { ok(w, []any{}) })
	mux.HandleFunc("GET /v1/api/torrents/requestdl", func(w http.ResponseWriter, _ *http.Request) {
		ok(w, f.server.URL+"/dld/debian-iso")
	})
	mux.HandleFunc("GET /dld/debian-iso", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "debian.iso", time.Time{}, strings.NewReader(isoContent))
	})
	mux.HandleFunc("POST /v1/api/torrents/controltorrent", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TorrentID int64  `json:"torrent_id"`
			Operation string `json:"operation"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		delete(f.items, body.TorrentID)
		f.deleted = append(f.deleted, body.TorrentID)
		f.mu.Unlock()
		ok(w, nil)
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTorBox) deletions() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.deleted...)
}

// syncBuffer collects logs from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fullStack struct {
	t       *testing.T
	baseURL string
	token   string
}

func (s fullStack) call(method, path string, body any, header map[string]string) (int, http.Header, []byte) {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(s.t, err)
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, s.baseURL+path, reader)
	require.NoError(s.t, err)
	request.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	for k, v := range header {
		request.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(s.t, err)
	defer func() { require.NoError(s.t, response.Body.Close()) }()
	payload, err := io.ReadAll(response.Body)
	require.NoError(s.t, err)
	return response.StatusCode, response.Header, payload
}

// TestFullStack drives every context through cmd: identity, job, provider
// with the real TorBox adapter against a TLS fake, delivery, and SQLite.
func TestFullStack_submit_download_release_and_no_secret_in_logs(t *testing.T) {
	t.Parallel()
	torbox := newFakeTorBox(t)
	host := strings.TrimPrefix(torbox.server.URL, "https://")
	environ := []string{
		"DEBROXY_DATA_DIR=" + t.TempDir(),
		"DEBROXY_SERVER_LISTEN=127.0.0.1:0",
		"DEBROXY_SERVER_PUBLIC_BASE_URL=https://debroxy.example.org",
		"DEBROXY_SERVER_SHUTDOWN_TIMEOUT=2s",
		"DEBROXY_LOG_LEVEL=debug",
		"DEBROXY_SIGNING_KEYS=k1",
		"DEBROXY_SIGNING_KEY_K1=" + secret32,
		"DEBROXY_PROVIDERS=torbox",
		"DEBROXY_PROVIDER_TORBOX_KIND=torbox",
		"DEBROXY_PROVIDER_TORBOX_API_KEY=torbox-key",
		"DEBROXY_PROVIDER_TORBOX_BASE_URL=https://" + host + "/v1/api",
		"DEBROXY_PROVIDER_TORBOX_CDN_PROBE_HOSTS=" + host,
	}
	cfg := mustConfig(t, environ)
	logs := &syncBuffer{}
	logger := newLogger(cfg.Log, logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, bootstrapAdmin(ctx, cfg, logger, "root", strings.NewReader(adminSecret+"\n")))
	roots := x509.NewCertPool()
	roots.AddCert(torbox.server.Certificate())
	process, err := startProcessWith(ctx, cfg, logger, processOptions{rootCAs: roots})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process.run(ctx) }()
	stack := fullStack{t: t, baseURL: "http://" + process.listener.Addr().String()}

	status, _, body := stack.call(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "root", "password": adminSecret}, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var login struct{ Token string }
	require.NoError(t, json.Unmarshal(body, &login))
	stack.token = login.Token

	magnet := "magnet:?xt=urn:btih:" + debianHash + "&dn=debian-netinst"
	status, _, body = stack.call(http.MethodPost, "/api/v1/jobs", map[string]any{"providerId": "torbox", "source": map[string]string{"type": "magnet", "magnet": magnet}}, nil)
	require.Equal(t, http.StatusCreated, status, string(body))
	var job struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(body, &job))
	assert.Equal(t, "ready", job.State, "a cached torrent is ready at once")

	status, _, body = stack.call(http.MethodGet, "/api/v1/jobs", nil, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"stale":false`, "refresh-on-read listed the live account")

	status, _, body = stack.call(http.MethodPost, "/api/v1/jobs/"+job.ID+"/links", map[string]any{"target": map[string]string{"fileId": "0"}}, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.NotContains(t, string(body), host, "no CDN or provider host in a link")
	var link struct{ URL string }
	require.NoError(t, json.Unmarshal(body, &link))
	linkURL, err := url.Parse(link.URL)
	require.NoError(t, err)

	download := fullStack{t: t, baseURL: stack.baseURL}
	status, header, body := download.call(http.MethodGet, linkURL.Path, nil, map[string]string{"Range": "bytes=2-7"})
	require.Equal(t, http.StatusPartialContent, status, string(body))
	assert.Equal(t, "234567", string(body))
	assert.Equal(t, fmt.Sprintf("bytes 2-7/%d", len(isoContent)), header.Get("Content-Range"))
	assert.Equal(t, "attachment; filename*=UTF-8''debian.iso", header.Get("Content-Disposition"))

	status, _, body = stack.call(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil, nil)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Eventually(t, func() bool { return len(torbox.deletions()) == 1 }, 5*time.Second, 50*time.Millisecond, "the outbox deletes the provider item")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}

	captured := logs.String()
	require.NotEmpty(t, captured)
	for name, secret := range map[string]string{
		"API key": "torbox-key", "signing key": secret32, "session token": login.Token,
		"admin password": adminSecret, "link token": strings.Split(strings.TrimPrefix(linkURL.Path, "/dl/"), "/")[0],
		"CDN path": "/dld/debian-iso",
	} {
		assert.NotContains(t, captured, secret, "the %s leaked into the logs", name)
	}
}
