package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenLink_caches_and_re_resolves_a_stale_link_once(t *testing.T) {
	t.Parallel()
	var staleHits, freshHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stale":
			staleHits.Add(1)
			w.WriteHeader(http.StatusGone)
		case "/fresh":
			freshHits.Add(1)
			_, _ = w.Write([]byte("bytes"))
		}
	}))
	defer server.Close()
	client, c := newTestClient(t, nil)
	cache := NewLinkCache(30*time.Minute, c)
	var resolves atomic.Int32
	links := []string{server.URL + "/stale", server.URL + "/fresh"}
	request := LinkRequest{
		CacheKey: "item/file",
		Method:   http.MethodGet,
		Resolve: func(context.Context) (SecretURL, error) {
			index := resolves.Add(1) - 1
			return ParseSecretURL(links[min(int(index), 1)])
		},
	}

	response, err := client.OpenLink(context.Background(), cache, request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	response, err = client.OpenLink(context.Background(), cache, request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, int32(2), resolves.Load(), "one resolve, one re-resolve; the second open used the cache")
	assert.Equal(t, int32(1), staleHits.Load())
	assert.Equal(t, int32(2), freshHits.Load())

	c.Advance(30 * time.Minute)
	response, err = client.OpenLink(context.Background(), cache, request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, int32(3), resolves.Load(), "entries live at most the cache maximum")
}

func TestOpenLink_gives_up_after_one_re_resolve(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	client, c := newTestClient(t, nil)
	var resolves atomic.Int32

	response, err := client.OpenLink(context.Background(), NewLinkCache(time.Minute, c), LinkRequest{
		CacheKey: "k", Method: http.MethodGet,
		Resolve: func(context.Context) (SecretURL, error) {
			resolves.Add(1)
			return ParseSecretURL(server.URL)
		},
	})

	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, int32(2), resolves.Load())
}
