package replay_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/httpclient/replay"
)

var fixtures = filepath.Join("..", "..", "..", "..", "docs", "discovery", "torbox")

func get(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(body)
}

func TestLoadExchange_parses_a_transcript(t *testing.T) {
	t.Parallel()

	exchange := replay.MustLoadExchange(t, filepath.Join(fixtures, "09-mylist-id-after-delete.http"))

	assert.Equal(t, http.MethodGet, exchange.Method)
	assert.Equal(t, "/v1/api/torrents/mylist", exchange.URL.Path)
	assert.Equal(t, http.StatusNotFound, exchange.Status)
	assert.Equal(t, "300", exchange.Header.Get("X-Ratelimit-Limit"))
	assert.JSONEq(t, `{"success":true,"error":"ITEM_NOT_FOUND","detail":"No torrents found for this user.","data":{}}`, string(exchange.Body))
}

func TestServer_matches_method_path_and_query_ignoring_the_token(t *testing.T) {
	t.Parallel()
	server := replay.NewServer(t, replay.MustLoadExchange(t, filepath.Join(fixtures, "25-requestdl-sintel-zip.http")))

	status, body := get(t, server.URL()+"/v1/api/torrents/requestdl?zip_link=true&torrent_id=116446015&token=REAL-KEY")

	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "Torrent download requested successfully")
	assert.Empty(t, server.Unserved())
	require.Len(t, server.Requests(), 1)
	assert.Equal(t, "REAL-KEY", server.Requests()[0].Query.Get("token"))
}

func TestServer_serves_repeated_requests_in_order(t *testing.T) {
	t.Parallel()
	server := replay.NewServer(t,
		replay.MustLoadExchange(t, filepath.Join(fixtures, "05-poll-webdl-item-09.http")),
		replay.MustLoadExchange(t, filepath.Join(fixtures, "05-poll-webdl-item-10.http")),
	)
	target := server.URL() + "/v1/api/webdl/mylist?id=1975283&bypass_cache=true"

	_, first := get(t, target)
	_, second := get(t, target)

	assert.Contains(t, first, `"download_state":"processing"`)
	assert.Contains(t, second, `"download_state":"completed"`)
}
