package realdebrid_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/httpclient/replay"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// unrestrictTo replays an unrestrict transcript whose download link points
// at the replay server instead of the real CDN.
func unrestrictTo(t *testing.T, h *harness, name, cdnPath string) replay.Exchange {
	t.Helper()
	exchange := fixture(t, name)
	var answer map[string]any
	require.NoError(t, json.Unmarshal(exchange.Body, &answer))
	answer["download"] = h.server.URL() + cdnPath
	body, err := json.Marshal(answer)
	require.NoError(t, err)
	return exchange.WithBody(body)
}

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

func TestOpenDeliveryStream_single_file_with_range(t *testing.T) {
	t.Parallel()

	for _, cdn := range []string{"04-cdn-range-1mib.http", "04-cdn-range-other-ua.http"} {
		t.Run(cdn, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, 0)
			h.server.Add(fixture(t, "02-poll-after-select-01.http"), unrestrictTo(t, h, "04-unrestrict-link.http", "/d/debian.iso"),
				cdnAt(t, h, cdn, http.MethodGet, "/d/debian.iso"))

			stream := open(t, h, in.OpenDeliveryStreamRequest{
				ProviderID: "realdebrid", Kind: in.KindTorrent, ProviderItemID: "4AL3JZAFXAIBS",
				Target: in.DeliveryTarget{FileID: "1"}, Method: http.MethodGet, Range: "bytes=0-1048575",
			})

			assert.Equal(t, http.StatusPartialContent, stream.Status)
			assert.True(t, stream.RangeSupported)
			assert.NotEmpty(t, stream.Headers["Content-Range"])
			requests := h.server.Requests()
			assert.Contains(t, string(requests[1].Body), "link=https%3A%2F%2Freal-debrid.com%2Fd%2F")
			assert.Equal(t, "bytes=0-1048575", requests[2].Header.Get("Range"))
			assert.Empty(t, requests[2].Header.Get("Authorization"))
		})
	}
}

func TestOpenDeliveryStream_head(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0)
	h.server.Add(fixture(t, "02-poll-after-select-01.http"), unrestrictTo(t, h, "04-unrestrict-link.http", "/d/debian.iso"),
		cdnAt(t, h, "04-cdn-head.http", http.MethodHead, "/d/debian.iso"))

	stream := open(t, h, in.OpenDeliveryStreamRequest{
		ProviderID: "realdebrid", Kind: in.KindTorrent, ProviderItemID: "4AL3JZAFXAIBS", Target: in.DeliveryTarget{FileID: "1"}, Method: http.MethodHead,
	})

	assert.Equal(t, http.StatusOK, stream.Status)
	assert.Equal(t, "792723456", stream.Headers["Content-Length"])
	assert.Equal(t, "application/force-download", stream.Headers["Content-Type"])
}

func TestOpenDeliveryStream_rar_bundle(t *testing.T) {
	t.Parallel()
	request := in.OpenDeliveryStreamRequest{
		ProviderID: "realdebrid", Kind: in.KindTorrent, ProviderItemID: "7ZZNB3UPAOEXG",
		Target: in.DeliveryTarget{Bundle: true}, Method: http.MethodGet, Range: "bytes=0-1048575",
	}

	t.Run("range_honoured", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0)
		h.server.Add(fixture(t, "26-poll-sintel-14.http"), unrestrictTo(t, h, "26-unrestrict-sintel-link.http", "/d/Sintel.rar"),
			cdnAt(t, h, "04-cdn-range-1mib.http", http.MethodGet, "/d/Sintel.rar"))

		stream := open(t, h, request)

		assert.Equal(t, http.StatusPartialContent, stream.Status)
		assert.True(t, stream.RangeSupported)
	})

	t.Run("range_ignored_falls_back_to_the_whole_body", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0)
		wholeBody := fixture(t, "04-cdn-head.http")
		h.server.Add(fixture(t, "26-poll-sintel-14.http"), unrestrictTo(t, h, "26-unrestrict-sintel-link.http", "/d/Sintel.rar"),
			wholeBody.WithRequest(http.MethodGet, h.server.URL()+"/d/Sintel.rar"))

		stream := open(t, h, request)

		assert.Equal(t, http.StatusOK, stream.Status)
		assert.False(t, stream.RangeSupported, "ASM-R15 refuted at runtime: ADR 0010 rule 20")
	})
}

func TestOpenDeliveryStream_target_must_match_the_selection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		info   string
		itemID string
		target in.DeliveryTarget
	}{
		{"file_of_a_multi_file_item", "26-poll-sintel-14.http", "7ZZNB3UPAOEXG", in.DeliveryTarget{FileID: "6"}},
		{"bundle_of_a_single_file_item", "27-info-cosmos.http", "4IDWK4PCUJZ4E", in.DeliveryTarget{Bundle: true}},
		{"another_file_of_a_single_file_item", "27-info-cosmos.http", "4IDWK4PCUJZ4E", in.DeliveryTarget{FileID: "2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, 0, fixture(t, tc.info))

			_, err := h.gateway.OpenDeliveryStream(context.Background(), in.OpenDeliveryStreamRequest{
				ProviderID: "realdebrid", Kind: in.KindTorrent, ProviderItemID: tc.itemID, Target: tc.target, Method: http.MethodGet,
			})

			var rejected in.ProviderRejectedError
			assert.ErrorAs(t, err, &rejected)
			assert.Len(t, h.server.Requests(), 1, "nothing is unrestricted")
		})
	}
}
