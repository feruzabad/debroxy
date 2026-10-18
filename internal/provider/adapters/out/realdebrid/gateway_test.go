package realdebrid_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/common/httpclient/replay"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/realdebrid"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

const (
	apiKey = "test-rd-token-SECRET-0123456789"
	api    = "https://api.real-debrid.com/rest/1.0"
)

var fixtureDir = filepath.Join("..", "..", "..", "..", "..", "docs", "discovery", "realdebrid")

// excludedFixtures are transcripts no test in this stage uses, with the reason.
var excludedFixtures = map[string]string{
	"01-auth-user.http":                "account info is not part of the gateway",
	"08-unrestrict-archive-org.http":   "web links are not enabled for Real-Debrid in v1 (DEC-16)",
	"08-unrestrict-debian-direct.http": "web links are not enabled for Real-Debrid in v1 (DEC-16)",
	"08-unrestrict-check.http":         "web links are not enabled for Real-Debrid in v1 (DEC-16)",
	"13-torrents-offset2.http":         "the adapter pages with page, not offset",
	"21-select-all.http":               "the adapter selects files by ID, never with files=all (RD-N09)",
	"27-select-bbb.http":               "the adapter selects files by ID, never with files=all (RD-N09)",
	"27-select-cosmos.http":            "a selection by ID; selectFor replays the same 204",
}

var (
	usedMu   sync.Mutex
	used     = map[string]bool{}
	fixtures = map[string]replay.Exchange{}
)

func TestMain(m *testing.M) {
	code := m.Run()
	// Only a full run can judge coverage; -run selects a subset.
	if code == 0 && flag.Lookup("test.run").Value.String() == "" {
		code = checkFixtureCoverage()
	}
	os.Exit(code)
}

func checkFixtureCoverage() int {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		fmt.Println("fixture coverage:", err)
		return 1
	}
	var missing []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasSuffix(name, ".http") && !used[name] && excludedFixtures[name] == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		fmt.Println("fixtures neither used by a test nor excluded:", strings.Join(missing, ", "))
		return 1
	}
	return 0
}

func fixture(t *testing.T, name string) replay.Exchange {
	t.Helper()
	usedMu.Lock()
	defer usedMu.Unlock()
	used[name] = true
	if e, ok := fixtures[name]; ok {
		return e
	}
	e := replay.MustLoadExchange(t, filepath.Join(fixtureDir, name))
	fixtures[name] = e
	return e
}

// selectFor replays a recorded 204 selection for another item ID.
func selectFor(t *testing.T, id string) replay.Exchange {
	t.Helper()
	return fixture(t, "02-select-files-all.http").WithRequest(http.MethodPost, api+"/torrents/selectFiles/"+id)
}

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

type harness struct {
	gateway *realdebrid.Gateway
	server  *replay.Server
	clock   *clock.Fixed
	logs    *syncBuffer
}

func newHarness(t *testing.T, pageSize int, exchanges ...replay.Exchange) *harness {
	t.Helper()
	server := replay.NewServer(t, exchanges...)
	fixed := clock.NewFixed(time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))
	logs := &syncBuffer{}
	gateway, err := realdebrid.New(realdebrid.Config{
		AccountID:      "realdebrid",
		BaseURL:        server.URL() + "/rest/1.0",
		APIKey:         apiKey,
		UserAgent:      "debroxy-test/1",
		Timeout:        5 * time.Second,
		Retry:          httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond},
		CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 50, OpenDuration: time.Minute},
		Egress:         httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets:    map[string]realdebrid.RateBudget{"general": {Requests: 250, Per: time.Minute}},
		Clock:          fixed,
		Logger:         slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Sleep: func(_ context.Context, d time.Duration) error {
			fixed.Advance(d)
			return nil
		},
		PageSize:        pageSize,
		CDNLinkCacheMax: 30 * time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.Empty(t, server.Unserved(), "every recorded answer is used")
		assert.NotContains(t, logs.String(), apiKey)
	})
	return &harness{gateway: gateway, server: server, clock: fixed, logs: logs}
}

func TestCreateRemoteItem_adds_selects_all_and_observes(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0, fixture(t, "02-add-magnet.http"), fixture(t, "02-poll-info-01.http"),
		fixture(t, "02-select-files-all.http"), fixture(t, "02-poll-after-select-01.http"))
	magnet := "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0"

	result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: magnet}, true)

	require.NoError(t, err)
	assert.Equal(t, "4AL3JZAFXAIBS", result.ProviderItemID)
	assert.Equal(t, "7acf8fb590b2060dd9c3146ef770169d593433b0", result.ProviderHash)
	assert.Equal(t, in.StateReady, result.Observation.State)
	assert.True(t, result.Observation.PerFileDelivery, "one selected file is delivered directly")
	assert.Nil(t, result.Observation.Bundle)

	requests := h.server.Requests()
	assert.Equal(t, "Bearer "+apiKey, requests[0].Header.Get("Authorization"))
	assert.Equal(t, "application/x-www-form-urlencoded", requests[0].Header.Get("Content-Type"))
	assert.Contains(t, string(requests[0].Body), "magnet=magnet%3A%3Fxt%3Durn%3Abtih%3A7acf")
	assert.Equal(t, http.MethodGet, requests[1].Method, "the file IDs are read before selecting")
	assert.Equal(t, "files=1", string(requests[2].Body), "all files selected at once by ID (DEC-15, RD-N09)")
}

// files=all can select nothing (RD-N09), so every file is named by ID.
func TestCreateRemoteItem_selects_every_file_by_id(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0, fixture(t, "20-add-torrent-avenge.http"), fixture(t, "20-poll-avenge-pre-01.http"),
		selectFor(t, "ACDIALU45Q7PS"), fixture(t, "20-poll-avenge-01.http"))

	_, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceTorrentFile, File: []byte("d4:info...e")}, false)

	require.NoError(t, err)
	selection := h.server.Requests()[2]
	assert.Equal(t, "/rest/1.0/torrents/selectFiles/ACDIALU45Q7PS", selection.Path)
	assert.Equal(t, "files=1%2C2%2C3%2C4%2C5%2C6%2C7%2C8%2C9%2C10%2C11%2C12%2C13%2C14%2C15%2C16%2C17%2C18", string(selection.Body))
}

func TestCreateRemoteItem_torrent_files(t *testing.T) {
	t.Parallel()

	// Each recorded item is read twice: before selecting (it is past the
	// selection already, so none is sent) and to observe it.
	for _, tc := range []struct {
		add, info string
		id        string
	}{
		{"03-add-same-as-torrentfile.http", "", "DI6NWHCOY5XAE"},
		{"20-add-torrent-avenge.http", "20-poll-avenge-01.http", "ACDIALU45Q7PS"},
		{"21-add-torrent-soundie13.http", "24-info-before-delete-rd-soundie13.http", "NGHAU27TFFGOS"},
		{"22-add-torrent-soundie1.http", "22-poll-soundie1-01.http", "6R344WOQR76AQ"},
		{"26-add-sintel.http", "26-poll-sintel-01.http", "7ZZNB3UPAOEXG"},
		{"27-add-bbb.http", "27-info-bbb.http", "JU4TNWHCAZXAA"},
		{"27-add-cosmos.http", "27-info-cosmos.http", "4IDWK4PCUJZ4E"},
	} {
		t.Run(tc.add, func(t *testing.T) {
			t.Parallel()
			info := fixture(t, "09-info-after-delete.http").WithRequest(http.MethodGet, api+"/torrents/info/"+tc.id)
			if tc.info != "" {
				info = fixture(t, tc.info)
			}
			h := newHarness(t, 0, fixture(t, tc.add), info, info)

			result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceTorrentFile, File: []byte("d4:info...e")}, false)

			require.NoError(t, err)
			assert.Equal(t, tc.id, result.ProviderItemID)
			assert.Equal(t, http.MethodPut, h.server.Requests()[0].Method)
			assert.Equal(t, "application/x-bittorrent", h.server.Requests()[0].Header.Get("Content-Type"))
			if tc.info == "" {
				assert.Equal(t, in.StateQueued, result.Observation.State, "a failed look-up reports queued")
			}
		})
	}
}

func TestCreateRemoteItem_no_provider_dedup(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0, fixture(t, "03-readd-same-magnet.http"), fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"),
		fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"))

	result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: "magnet:?xt=urn:btih:7acf"}, false)

	require.NoError(t, err)
	assert.Equal(t, "XOW3B5KGBF226", result.ProviderItemID, "the same content gets a new item (RD-N01)")
	assert.Equal(t, in.StateAwaitingFileSelection, result.Observation.State, "the recording was made before the selection took effect; reading it selects again")
}

func TestDescribeRemoteItem_maps_every_observed_status(t *testing.T) {
	t.Parallel()
	var testCases []struct {
		fixture string
		state   in.State
	}
	add := func(state in.State, names ...string) {
		for _, name := range names {
			testCases = append(testCases, struct {
				fixture string
				state   in.State
			}{name, state})
		}
	}
	add(in.StateQueued, "20-poll-avenge-01.http", "26-poll-sintel-01.http")
	for i := 2; i <= 27; i++ {
		add(in.StateDownloading, fmt.Sprintf("20-poll-avenge-%02d.http", i))
	}
	for i := 2; i <= 11; i++ {
		add(in.StateDownloading, fmt.Sprintf("26-poll-sintel-%02d.http", i))
	}
	for i := 1; i <= 6; i++ {
		add(in.StateDownloading, fmt.Sprintf("22-poll-soundie1-%02d.http", i))
	}
	add(in.StateDownloading, "20-status-avenge.http", "24-info-before-delete-rd-avenge.http", "24-info-before-delete-rd-soundie1.http", "24-info-before-delete-rd-soundie13.http")
	add(in.StateProcessing, "26-poll-sintel-12.http", "26-poll-sintel-13.http")
	add(in.StateReady, "26-poll-sintel-14.http", "02-poll-after-select-01.http", "27-info-bbb.http", "27-info-cosmos.http")

	for _, tc := range testCases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			exchange := fixture(t, tc.fixture)
			h := newHarness(t, 0, exchange)

			observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, strings.TrimPrefix(exchange.URL.Path, "/rest/1.0/torrents/info/"))

			require.NoError(t, err)
			assert.Equal(t, tc.state, observation.State)
			assert.GreaterOrEqual(t, observation.Telemetry.Progress, 0.0)
			assert.LessOrEqual(t, observation.Telemetry.Progress, 1.0)
		})
	}
}

func TestDescribeRemoteItem_waiting_items_get_their_files_selected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ info, selection string }{
		{"02-poll-info-01.http", "02-select-files-all.http"},
		{"20-poll-avenge-pre-01.http", "20-select-subset.http"},
		{"22-info-files.http", "22-select-media-only.http"},
		{"26-sintel-info-pre.http", "26-select-subset.http"},
		{"03-dup-torrentfile-info.http", ""},
	} {
		t.Run(tc.info, func(t *testing.T) {
			t.Parallel()
			info := fixture(t, tc.info)
			id := strings.TrimPrefix(info.URL.Path, "/rest/1.0/torrents/info/")
			selection := selectFor(t, id)
			if tc.selection != "" {
				selection = fixture(t, tc.selection)
			}
			h := newHarness(t, 0, info, selection)

			observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, id)

			require.NoError(t, err)
			assert.Equal(t, in.StateAwaitingFileSelection, observation.State)
			assert.Empty(t, observation.Files, "no file is selected yet")
		})
	}
}

func TestDescribeRemoteItem_delivery_targets_follow_the_selection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fixture string
		files   int
		perFile bool
		bundle  *in.Bundle
	}{
		{"27-info-cosmos.http", 1, true, nil},
		{"27-info-bbb.http", 3, false, &in.Bundle{Format: in.BundleRAR, RangeSupported: true}},
		{"26-poll-sintel-14.http", 3, false, &in.Bundle{Format: in.BundleRAR, RangeSupported: true}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			exchange := fixture(t, tc.fixture)
			h := newHarness(t, 0, exchange)

			observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, strings.TrimPrefix(exchange.URL.Path, "/rest/1.0/torrents/info/"))

			require.NoError(t, err)
			assert.Len(t, observation.Files, tc.files, "only selected files")
			assert.Equal(t, tc.perFile, observation.PerFileDelivery)
			assert.Equal(t, tc.bundle, observation.Bundle)
		})
	}

	t.Run("file_ids_and_paths", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0, fixture(t, "26-poll-sintel-14.http"))

		observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "7ZZNB3UPAOEXG")

		require.NoError(t, err)
		assert.Contains(t, observation.Files, in.RemoteFile{FileID: "6", Path: "Sintel.mp4", SizeBytes: 129241752})
		assert.Equal(t, "Sintel", observation.Name)
	})
}

func TestDescribeRemoteItem_not_found(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0, fixture(t, "09-info-after-delete.http"))

	_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "4AL3JZAFXAIBS")

	assert.ErrorIs(t, err, in.ErrRemoteItemNotFound)
}

func TestListRemoteItems_walks_pages_and_reads_info(t *testing.T) {
	t.Parallel()
	page1 := fixture(t, "13-torrents-limit1-page1.http")
	page2 := fixture(t, "13-torrents-limit1-page2.http")
	end := fixture(t, "13-torrents-page-beyond.http").WithRequest(http.MethodGet, api+"/torrents?limit=1&page=3")
	h := newHarness(t, 1, page1, page2, end,
		fixture(t, "03-dup-torrentfile-info.http"), selectFor(t, "DI6NWHCOY5XAE"),
		fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"))

	listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})

	require.NoError(t, err)
	assert.True(t, listing.Complete)
	require.Len(t, listing.Observations, 2)
	assert.Equal(t, in.StateAwaitingFileSelection, listing.Observations[0].State, "info, not the stale list status (RD-N04)")
}

func TestListRemoteItems_reads_finished_items_once(t *testing.T) {
	t.Parallel()
	all := fixture(t, "13-torrents-all.http").WithRequest(http.MethodGet, api+"/torrents?limit=100&page=1")
	h := newHarness(t, 0,
		all, fixture(t, "03-dup-torrentfile-info.http"), selectFor(t, "DI6NWHCOY5XAE"),
		fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"),
		fixture(t, "02-poll-after-select-01.http"),
		// Second listing: the downloaded item comes from the cache, the waiting ones are read again.
		all, fixture(t, "03-dup-torrentfile-info.http"), selectFor(t, "DI6NWHCOY5XAE"),
		fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"),
	)

	first, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})
	require.NoError(t, err)
	second, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})
	require.NoError(t, err)

	assert.Len(t, first.Observations, 3)
	assert.Equal(t, first.Observations, second.Observations)
}

func TestListRemoteItems_empty_account(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"02-baseline-torrents.http", "09-final-torrents.http", "28-final-torrents.http"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, 0, fixture(t, name).WithRequest(http.MethodGet, api+"/torrents?limit=100&page=1"))

			listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})

			require.NoError(t, err)
			assert.True(t, listing.Complete)
			assert.Empty(t, listing.Observations)
		})
	}
}

func TestListRemoteItems_a_failed_info_makes_it_incomplete(t *testing.T) {
	t.Parallel()
	all := fixture(t, "13-torrents-all.http").WithRequest(http.MethodGet, api+"/torrents?limit=100&page=1")
	failure := fixture(t, "01-auth-invalid-key.http")
	failure.Status = http.StatusServiceUnavailable
	failure.Body = []byte("")
	h := newHarness(t, 0,
		all,
		failure.WithRequest(http.MethodGet, api+"/torrents/info/DI6NWHCOY5XAE"),
		failure.WithRequest(http.MethodGet, api+"/torrents/info/DI6NWHCOY5XAE"),
		fixture(t, "03-dup-magnet-info.http"), selectFor(t, "XOW3B5KGBF226"),
		fixture(t, "02-poll-after-select-01.http"),
	)

	listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})

	require.NoError(t, err)
	assert.False(t, listing.Complete)
	assert.Len(t, listing.Observations, 2)
}

func TestDeleteRemoteItem(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"09-delete-4AL3JZAFXAIBS.http", "09-delete-DI6NWHCOY5XAE.http", "09-delete-XOW3B5KGBF226.http",
		"24-delete-rd-avenge.http", "24-delete-rd-soundie1.http", "24-delete-rd-soundie13.http",
		"28-delete-bbb.http", "28-delete-cosmos.http", "28-delete-sintel.http",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			exchange := fixture(t, name)
			h := newHarness(t, 0, exchange)

			err := h.gateway.DeleteRemoteItem(context.Background(), in.KindTorrent, strings.TrimPrefix(exchange.URL.Path, "/rest/1.0/torrents/delete/"))

			require.NoError(t, err)
		})
	}

	t.Run("again_is_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0, fixture(t, "09-delete-again.http"))

		err := h.gateway.DeleteRemoteItem(context.Background(), in.KindTorrent, "4AL3JZAFXAIBS")

		assert.ErrorIs(t, err, in.ErrRemoteItemNotFound)
	})
}

func TestSweepDownloadHistory(t *testing.T) {
	t.Parallel()

	t.Run("deletes_old_entries", func(t *testing.T) {
		t.Parallel()
		// The entry was generated 2026-10-11T15:38 (labelled UTC); the clock is two days later.
		history := fixture(t, "09-downloads-history.http").WithRequest(http.MethodGet, api+"/downloads?limit=100&page=1")
		h := newHarness(t, 0, history, fixture(t, "09-delete-download-history.http"))

		deleted, err := h.gateway.SweepDownloadHistory(context.Background())

		require.NoError(t, err)
		assert.Equal(t, 1, deleted)
	})

	t.Run("keeps_recent_entries", func(t *testing.T) {
		t.Parallel()
		history := fixture(t, "09-downloads-history.http").WithRequest(http.MethodGet, api+"/downloads?limit=100&page=1")
		h := newHarness(t, 0, history)
		h.clock.Set(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))

		deleted, err := h.gateway.SweepDownloadHistory(context.Background())

		require.NoError(t, err)
		assert.Zero(t, deleted, "within 24 h plus the time zone margin (RD-N03)")
	})

	for _, name := range []string{"02-baseline-downloads.http", "09-final-downloads.http", "28-final-downloads.http"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, 0, fixture(t, name).WithRequest(http.MethodGet, api+"/downloads?limit=100&page=1"))

			deleted, err := h.gateway.SweepDownloadHistory(context.Background())

			require.NoError(t, err)
			assert.Zero(t, deleted)
		})
	}
}

func TestHosterCatalogue(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 0, fixture(t, "08-hosts-domains.http"))

	catalogue, err := h.gateway.HosterCatalogue(context.Background())

	require.NoError(t, err)
	assert.Len(t, catalogue.Hosters, 281)
}

func TestErrors(t *testing.T) {
	t.Parallel()

	t.Run("bad_token", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0, fixture(t, "01-auth-invalid-key.http").WithRequest(http.MethodGet, api+"/torrents/info/X"))

		_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "X")

		assert.Equal(t, in.ProviderUnavailableError{Provider: "realdebrid", Cause: "credentials rejected"}, err)
		assert.Contains(t, h.logs.String(), "operator_alert")
		assert.NotContains(t, err.Error(), apiKey)
	})

	t.Run("disabled_endpoint", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0, fixture(t, "06-instant-availability.http").WithRequest(http.MethodGet, api+"/torrents/info/X"))

		_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "X")

		assert.Equal(t, in.ProviderRejectedError{Provider: "realdebrid", Detail: "endpoint disabled by the provider"}, err)
	})

	t.Run("no_cached_check_and_no_webhooks", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, 0)

		_, cachedErr := h.gateway.CheckCached(context.Background(), in.KindTorrent, []string{"x"})
		_, webhookErr := h.gateway.TranslateWebhook(nil, []byte(`{}`))

		assert.Error(t, cachedErr)
		assert.ErrorIs(t, webhookErr, in.ErrWebhookRejected)
	})
}
