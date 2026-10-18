package torbox_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/httpclient/replay"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// resolveTo replays a requestdl transcript whose link points at the replay
// server instead of the real CDN (the transcripts redact the real one).
func resolveTo(t *testing.T, h *harness, name, cdnPath string) replay.Exchange {
	t.Helper()
	exchange := fixture(t, name)
	var env map[string]any
	require.NoError(t, json.Unmarshal(exchange.Body, &env))
	env["data"] = h.server.URL() + cdnPath
	body, err := json.Marshal(env)
	require.NoError(t, err)
	return exchange.WithBody(body)
}

// cdnAt replays a CDN transcript at a path of the replay server.
func cdnAt(t *testing.T, h *harness, name, method, cdnPath string) replay.Exchange {
	t.Helper()
	return fixture(t, name).WithRequest(method, h.server.URL()+cdnPath)
}

func open(t *testing.T, h *harness, request in.OpenDeliveryStreamRequest) in.DeliveryStream {
	t.Helper()
	stream, err := h.gateway.OpenDeliveryStream(context.Background(), request)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, stream.Body)
	require.NoError(t, err)
	require.NoError(t, stream.Body.Close())
	return stream
}

const debianFile = "/dld/d05be61c-9d9c-40bc-a419-124376cd66af"

func TestOpenDeliveryStream_file_with_range(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.Add(resolveTo(t, h, "04-requestdl-token-param.http", debianFile), cdnAt(t, h, "04-cdn-range-1mib.http", http.MethodGet, debianFile))

	stream := open(t, h, in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "116409037",
		Target: in.DeliveryTarget{FileID: "0"}, Method: http.MethodGet, Range: "bytes=0-1048575",
	})

	assert.Equal(t, http.StatusPartialContent, stream.Status)
	assert.True(t, stream.RangeSupported)
	assert.Equal(t, map[string]string{
		"Content-Range":  "bytes 0-1048575/792723456",
		"Content-Type":   "application/octet-stream",
		"Etag":           `"6aa6157b-2f400000"`,
		"Content-Length": "28",
	}, normalizeHeaders(stream.Headers), "only whitelisted headers; the replayed body is the transcript's placeholder")

	requests := h.server.Requests()
	require.Len(t, requests, 2)
	resolve, cdn := requests[0], requests[1]
	assert.Equal(t, apiKey, resolve.Query.Get("token"), "requestdl needs the key in the query (torbox.md T07)")
	assert.Equal(t, "0", resolve.Query.Get("file_id"))
	assert.Empty(t, resolve.Query.Get("user_ip"), "user_ip is never sent (OQ-05)")
	assert.Equal(t, "bytes=0-1048575", cdn.Header.Get("Range"))
	assert.Equal(t, "debroxy-test/1", cdn.Header.Get("User-Agent"))
	assert.Empty(t, cdn.Header.Get("Authorization"), "the CDN request carries no API credentials")
}

// normalizeHeaders maps canonical names (ETag becomes Etag in http.Header).
func normalizeHeaders(headers map[string]string) map[string]string {
	result := map[string]string{}
	for name, value := range headers {
		result[http.CanonicalHeaderKey(name)] = value
	}
	return result
}

func TestOpenDeliveryStream_head_requests(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		resolve, cdn, cdnPath string
		target                in.DeliveryTarget
		itemID                string
	}{
		{"04-requestdl-token-param.http", "04-cdn-head.http", debianFile, in.DeliveryTarget{FileID: "0"}, "116409037"},
		{"25-requestdl-sintel-file10.http", "25-cdn-sintel-file10-head.http", "/dld/sintel10", in.DeliveryTarget{FileID: "10"}, "116446015"},
		{"04-requestdl-zip.http", "04-cdn-zip-head.http", "/zip/debian", in.DeliveryTarget{Bundle: true}, "116409037"},
		{"25-requestdl-sintel-zip.http", "25-cdn-sintel-zip-head.http", "/zip/sintel", in.DeliveryTarget{Bundle: true}, "116446015"},
	} {
		t.Run(tc.cdn, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.server.Add(resolveTo(t, h, tc.resolve, tc.cdnPath), cdnAt(t, h, tc.cdn, http.MethodHead, tc.cdnPath))

			stream := open(t, h, in.OpenDeliveryStreamRequest{
				ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: tc.itemID, Target: tc.target, Method: http.MethodHead,
			})

			assert.Equal(t, http.StatusOK, stream.Status)
			assert.NotEmpty(t, stream.Headers["Content-Length"])
			assert.Equal(t, !tc.target.Bundle, stream.RangeSupported, "a zip never supports ranges")
		})
	}
}

func TestOpenDeliveryStream_zip_ignores_range(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.Add(resolveTo(t, h, "25-requestdl-sintel-zip.http", "/zip/sintel"), cdnAt(t, h, "25-cdn-sintel-zip-range.http", http.MethodGet, "/zip/sintel"))

	stream := open(t, h, in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "116446015",
		Target: in.DeliveryTarget{Bundle: true}, Method: http.MethodGet, Range: "bytes=0-65535",
	})

	assert.Equal(t, http.StatusOK, stream.Status)
	assert.False(t, stream.RangeSupported)
	assert.Empty(t, h.server.Requests()[1].Header.Get("Range"), "Range is not forwarded for a zip (C-16)")
	assert.Equal(t, "true", h.server.Requests()[0].Query.Get("zip_link"))
}

func TestOpenDeliveryStream_web_item(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.server.Add(resolveTo(t, h, "08-webdl-requestdl.http", "/dld/web"), cdnAt(t, h, "04-cdn-head.http", http.MethodHead, "/dld/web"))

	stream := open(t, h, in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindWeb, ProviderItemID: "1975274", Target: in.DeliveryTarget{FileID: "0"}, Method: http.MethodHead,
	})

	assert.Equal(t, http.StatusOK, stream.Status)
	assert.Equal(t, "1975274", h.server.Requests()[0].Query.Get("web_id"))
}

func TestOpenDeliveryStream_reuses_the_link_and_re_resolves_a_stale_one(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	gone := fixture(t, "04-cdn-head.http")
	gone.Status = http.StatusGone
	request := in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "116409037", Target: in.DeliveryTarget{FileID: "0"}, Method: http.MethodHead,
	}
	// The recording with user_ip stands in for the second resolve; debroxy sends no user_ip.
	secondResolve := resolveTo(t, h, "04-requestdl-userip.http", "/dld/fresh").
		WithRequest(http.MethodGet, "https://api.torbox.app/v1/api/torrents/requestdl?torrent_id=116409037&file_id=0")
	h.server.Add(
		resolveTo(t, h, "04-requestdl-token-param.http", debianFile),
		cdnAt(t, h, "04-cdn-head.http", http.MethodHead, debianFile),
		cdnAt(t, h, "04-cdn-head.http", http.MethodHead, debianFile),
		gone.WithRequest(http.MethodHead, h.server.URL()+debianFile),
		secondResolve,
		cdnAt(t, h, "04-cdn-head.http", http.MethodHead, "/dld/fresh"),
	)

	open(t, h, request)
	open(t, h, request)
	stream := open(t, h, request)

	assert.Equal(t, http.StatusOK, stream.Status)
	var resolves int
	for _, r := range h.server.Requests() {
		if strings.HasSuffix(r.Path, "/requestdl") {
			resolves++
		}
	}
	assert.Equal(t, 2, resolves, "cached for the second open; resolved again after the 410")
}

func TestOpenDeliveryStream_upstream_failure_is_provider_unavailable(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	broken := fixture(t, "04-cdn-head.http")
	broken.Status = http.StatusBadGateway
	h.server.Add(resolveTo(t, h, "04-requestdl-token-param.http", debianFile),
		broken.WithRequest(http.MethodHead, h.server.URL()+debianFile),
		broken.WithRequest(http.MethodHead, h.server.URL()+debianFile))

	_, err := h.gateway.OpenDeliveryStream(context.Background(), in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: "116409037", Target: in.DeliveryTarget{FileID: "0"}, Method: http.MethodHead,
	})

	var unavailable in.ProviderUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.NotContains(t, err.Error(), apiKey)
	assert.NotContains(t, err.Error(), debianFile, "no CDN path in errors")
}
