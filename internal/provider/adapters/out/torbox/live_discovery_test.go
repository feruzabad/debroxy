//go:build discovery

package torbox_test

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
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/torbox"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// TestLive_read_only runs against the real TorBox API with TORBOX_API_KEY:
// go test -tags discovery ./internal/provider/adapters/out/torbox/
// It only reads: listings, the hoster catalogue and a cached check.
func TestLive_read_only(t *testing.T) {
	key := os.Getenv("TORBOX_API_KEY")
	if key == "" {
		t.Skip("TORBOX_API_KEY not set")
	}
	gateway, err := torbox.New(torbox.Config{
		AccountID: "torbox", BaseURL: "https://api.torbox.app/v1/api", APIKey: key,
		UserAgent: "debroxy-discovery/0.1", Timeout: 30 * time.Second,
		Retry:          httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Second},
		CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: time.Minute},
		Egress:         httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets: map[string]torbox.RateBudget{
			"general": {Requests: 300, Per: time.Minute}, "uncachedCreate": {Requests: 60, Per: time.Hour}, "cdnZip": {Requests: 60, Per: time.Minute},
		},
		Clock: clock.System{}, Logger: slog.New(slog.DiscardHandler), CDNLinkCacheMax: 30 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()

	listing, err := gateway.ListRemoteItems(ctx, []in.Kind{in.KindTorrent, in.KindWeb})
	require.NoError(t, err)
	assert.True(t, listing.Complete)
	for _, observation := range listing.Observations {
		assert.NotEqual(t, in.StateUnknown, observation.State, "unmapped state on item %s", observation.ProviderItemID)
	}
	t.Logf("listed %d items", len(listing.Observations))

	catalogue, err := gateway.HosterCatalogue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, catalogue.Hosters)

	cached, err := gateway.CheckCached(ctx, in.KindTorrent, []string{"7acf8fb590b2060dd9c3146ef770169d593433b0"})
	require.NoError(t, err)
	t.Logf("debian netinst cached: %v", cached.Items[0].Cached)
}

// TestLive_stream_one_mebibyte adds the cached Debian netinst magnet, reads
// at most 1 MiB through the CDN, and deletes the item again.
func TestLive_stream_one_mebibyte(t *testing.T) {
	key := os.Getenv("TORBOX_API_KEY")
	if key == "" {
		t.Skip("TORBOX_API_KEY not set")
	}
	gateway, err := torbox.New(torbox.Config{
		AccountID: "torbox", BaseURL: "https://api.torbox.app/v1/api", APIKey: key,
		UserAgent: "debroxy-discovery/0.1", Timeout: 30 * time.Second,
		Retry:          httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Second},
		CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: time.Minute},
		Egress:         httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets: map[string]torbox.RateBudget{
			"general": {Requests: 300, Per: time.Minute}, "uncachedCreate": {Requests: 60, Per: time.Hour}, "cdnZip": {Requests: 60, Per: time.Minute},
		},
		Clock: clock.System{}, Logger: slog.New(slog.DiscardHandler), CDNLinkCacheMax: 30 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, gateway.CheckEgress(ctx), "API and CDN hosts reachable over IPv4")

	created, err := gateway.CreateRemoteItem(ctx, in.Source{Type: in.SourceMagnet,
		Magnet: "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian-13.7.0-amd64-netinst.iso"}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, gateway.DeleteRemoteItem(context.Background(), in.KindTorrent, created.ProviderItemID))
	})
	require.Equal(t, in.StateReady, created.Observation.State)
	require.NotEmpty(t, created.Observation.Files)

	stream, err := gateway.OpenDeliveryStream(ctx, in.OpenDeliveryStreamRequest{
		ProviderID: "torbox", Kind: in.KindTorrent, ProviderItemID: created.ProviderItemID,
		Target: in.DeliveryTarget{FileID: created.Observation.Files[0].FileID}, Method: http.MethodGet, Range: "bytes=0-1048575",
	})
	require.NoError(t, err)
	read, err := io.Copy(io.Discard, io.LimitReader(stream.Body, 1<<20))
	require.NoError(t, stream.Body.Close())
	require.NoError(t, err)
	assert.Equal(t, http.StatusPartialContent, stream.Status)
	assert.True(t, stream.RangeSupported)
	assert.Equal(t, int64(1<<20), read)
	t.Logf("headers: %v", stream.Headers)
}
