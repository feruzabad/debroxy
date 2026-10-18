//go:build discovery

package realdebrid_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/realdebrid"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// TestLive_read_only runs against the real Real-Debrid API with REALDEBRID_API_KEY:
// go test -tags discovery ./internal/provider/adapters/out/realdebrid/
// It only reads: the torrent listing and the hoster domains.
func TestLive_read_only(t *testing.T) {
	key := os.Getenv("REALDEBRID_API_KEY")
	if key == "" {
		t.Skip("REALDEBRID_API_KEY not set")
	}
	gateway, err := realdebrid.New(realdebrid.Config{
		AccountID: "realdebrid", BaseURL: "https://api.real-debrid.com/rest/1.0", APIKey: key,
		UserAgent: "debroxy-discovery/0.1", Timeout: 30 * time.Second,
		Retry:          httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Second},
		CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: time.Minute},
		Egress:         httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets:    map[string]realdebrid.RateBudget{"general": {Requests: 250, Per: time.Minute}},
		Clock:          clock.System{}, Logger: slog.New(slog.DiscardHandler), CDNLinkCacheMax: 30 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()

	listing, err := gateway.ListRemoteItems(ctx, []in.Kind{in.KindTorrent})
	require.NoError(t, err)
	assert.True(t, listing.Complete)
	for _, observation := range listing.Observations {
		assert.NotEqual(t, in.StateUnknown, observation.State, "unmapped status on item %s", observation.ProviderItemID)
	}
	t.Logf("listed %d items", len(listing.Observations))

	catalogue, err := gateway.HosterCatalogue(ctx)
	require.NoError(t, err)
	t.Logf("%d hoster domains", len(catalogue.Hosters))
}

// TestLive_stream_one_mebibyte adds the cached Debian netinst magnet, reads
// at most 1 MiB through the CDN, and deletes the item again. The unrestrict
// leaves a /downloads entry, which the daily sweep removes after 38 h.
func TestLive_stream_one_mebibyte(t *testing.T) {
	key := os.Getenv("REALDEBRID_API_KEY")
	if key == "" {
		t.Skip("REALDEBRID_API_KEY not set")
	}
	gateway, err := realdebrid.New(realdebrid.Config{
		AccountID: "realdebrid", BaseURL: "https://api.real-debrid.com/rest/1.0", APIKey: key,
		UserAgent: "debroxy-discovery/0.1", Timeout: 30 * time.Second,
		Retry:          httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Second},
		CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: time.Minute},
		Egress:         httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets:    map[string]realdebrid.RateBudget{"general": {Requests: 250, Per: time.Minute}},
		Clock:          clock.System{}, Logger: slog.New(slog.DiscardHandler), CDNLinkCacheMax: 30 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, gateway.CheckEgress(ctx), "API and CDN hosts reachable over IPv4")

	created, err := gateway.CreateRemoteItem(ctx, in.Source{Type: in.SourceMagnet,
		Magnet: "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian-13.7.0-amd64-netinst.iso"}, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, gateway.DeleteRemoteItem(context.Background(), in.KindTorrent, created.ProviderItemID))
	})

	observation := created.Observation
	for deadline := time.Now().Add(30 * time.Second); observation.State != in.StateReady && time.Now().Before(deadline); {
		time.Sleep(2 * time.Second)
		observation, err = gateway.DescribeRemoteItem(ctx, in.KindTorrent, created.ProviderItemID)
		require.NoError(t, err)
	}
	require.Equal(t, in.StateReady, observation.State)
	require.Len(t, observation.Files, 1)

	stream, err := gateway.OpenDeliveryStream(ctx, in.OpenDeliveryStreamRequest{
		ProviderID: "realdebrid", Kind: in.KindTorrent, ProviderItemID: created.ProviderItemID,
		Target: in.DeliveryTarget{FileID: observation.Files[0].FileID}, Method: http.MethodGet, Range: "bytes=0-1048575",
	})
	require.NoError(t, err)
	read, err := io.Copy(io.Discard, io.LimitReader(stream.Body, 1<<20))
	require.NoError(t, stream.Body.Close())
	require.NoError(t, err)
	assert.Equal(t, http.StatusPartialContent, stream.Status)
	assert.Equal(t, int64(1<<20), read)
	t.Logf("headers: %v", stream.Headers)
}
