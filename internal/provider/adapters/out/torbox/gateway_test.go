package torbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/httpclient"
	"github.com/feruzabad/debroxy/internal/common/httpclient/replay"
	"github.com/feruzabad/debroxy/internal/provider/adapters/out/torbox"
	"github.com/feruzabad/debroxy/internal/provider/ports/in"
	"github.com/feruzabad/debroxy/internal/provider/ports/out"
)

const apiKey = "test-api-key-SECRET-0123456789"

var fixtureDir = filepath.Join("..", "..", "..", "..", "..", "docs", "discovery", "torbox")

// excludedFixtures are transcripts no test in this stage uses, with the reason.
var excludedFixtures = map[string]string{
	"01-auth-user-me.http":                       "account info is not part of the gateway",
	"02-baseline-usenet-mylist.http":             "usenet is deferred to v2 (C8-01)",
	"09-final-usenet-mylist.http":                "usenet is deferred to v2 (C8-01)",
	"06-usenet-checkcached.http":                 "usenet is deferred to v2 (C8-01)",
	"07-usenet-checkcached-md5.http":             "usenet is deferred to v2 (C8-01)",
	"06-webdl-checkcached.http":                  "web submits never check the cache (ADR 0013 rule 14)",
	"08-webdl-checkcached-md5url.http":           "web submits never check the cache (ADR 0013 rule 14)",
	"09-control-reannounce.http":                 "reannounce is not offered in v1 (OQ-10)",
	"04-requestdl-header-only.http":              "used as a 422 body on another path; requestdl itself is Stage 7",
	"05-status-multifile.http":                   "same state as 05-poll-multifile-03",
	"05-status-multifile-final.http":             "same state as 05-poll-multifile-03",
	"05-poll-multifile-after-reannounce-01.http": "same state as 05-poll-multifile-03",
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

// checkFixtureCoverage fails the run when a transcript is neither used nor
// excluded with a reason (Stage 5: every relevant fixture is exercised).
func checkFixtureCoverage() int {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		fmt.Println("fixture coverage:", err)
		return 1
	}
	var missing []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".http") && !strings.HasSuffix(name, ".json") {
			continue
		}
		if !used[name] && excludedFixtures[name] == "" {
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

func fixtureBody(t *testing.T, name string) []byte {
	t.Helper()
	usedMu.Lock()
	used[name] = true
	usedMu.Unlock()
	body, err := os.ReadFile(filepath.Join(fixtureDir, name))
	require.NoError(t, err)
	return body
}

// itemData extracts the envelope's data object of a mylist?id= transcript.
func itemData(t *testing.T, name string) json.RawMessage {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, name).Body, &env))
	return env.Data
}

func listBody(t *testing.T, items ...json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(items)
	require.NoError(t, err)
	return []byte(`{"success":true,"error":null,"detail":"list","data":` + string(data) + `}`)
}

type harness struct {
	gateway *torbox.Gateway
	server  *replay.Server
	clock   *clock.Fixed
	logs    *syncBuffer
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

// newHarness: the fixed clock sits before the transcripts' rate-limit resets,
// so their headers are valid.
func newHarness(t *testing.T, exchanges ...replay.Exchange) *harness {
	t.Helper()
	return newHarnessWithBudgets(t, map[string]torbox.RateBudget{
		"general":        {Requests: 300, Per: time.Minute},
		"uncachedCreate": {Requests: 60, Per: time.Hour},
		"cdnZip":         {Requests: 60, Per: time.Minute},
	}, exchanges...)
}

func newHarnessWithBudgets(t *testing.T, budgets map[string]torbox.RateBudget, exchanges ...replay.Exchange) *harness {
	t.Helper()
	return newHarnessWith(t, budgets, 0, exchanges...)
}

func newHarnessWithPageSize(t *testing.T, pageSize int, exchanges ...replay.Exchange) *harness {
	t.Helper()
	return newHarnessWith(t, map[string]torbox.RateBudget{
		"general": {Requests: 300, Per: time.Minute}, "uncachedCreate": {Requests: 60, Per: time.Hour}, "cdnZip": {Requests: 60, Per: time.Minute},
	}, pageSize, exchanges...)
}

func newHarnessWith(t *testing.T, budgets map[string]torbox.RateBudget, pageSize int, exchanges ...replay.Exchange) *harness {
	t.Helper()
	server := replay.NewServer(t, exchanges...)
	fixed := clock.NewFixed(time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC))
	logs := &syncBuffer{}
	gateway, err := torbox.New(torbox.Config{
		AccountID:       "torbox",
		BaseURL:         server.URL() + "/v1/api",
		APIKey:          apiKey,
		UserAgent:       "debroxy-test/1",
		Timeout:         5 * time.Second,
		Retry:           httpclient.RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond},
		CircuitBreaker:  httpclient.CircuitBreakerConfig{FailureThreshold: 50, OpenDuration: time.Minute},
		Egress:          httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets:     budgets,
		CDNLinkCacheMax: 30 * time.Minute,
		PageSize:        pageSize,
		Clock:           fixed,
		Logger:          slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Sleep: func(_ context.Context, d time.Duration) error {
			fixed.Advance(d)
			return nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.Empty(t, server.Unserved(), "every recorded answer is used")
		assert.NotContains(t, logs.String(), apiKey, "the API key never reaches the log")
	})
	return &harness{gateway: gateway, server: server, clock: fixed, logs: logs}
}

func TestCreateRemoteItem_uncached_magnet(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "02-create-magnet-uncached.http"), fixture(t, "02-poll-uncached-01.http"))
	magnet := "magnet:?xt=urn:btih:dcd7316b0fc937cd45d8f56eea8021043bfa74a0&dn=debian-mac-13.7.0-amd64-netinst.iso"

	result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: magnet}, false)

	require.NoError(t, err)
	assert.Equal(t, "116409053", result.ProviderItemID)
	assert.Equal(t, "dcd7316b0fc937cd45d8f56eea8021043bfa74a0", result.ProviderHash)
	assert.Equal(t, in.StateReady, result.Observation.State)
	assert.Nil(t, result.Observation.Telemetry.ETA, "eta 8640000 means unknown")
	require.NotNil(t, result.Observation.ExpiresAt)
	assert.Equal(t, time.Date(2026, 10, 12, 13, 28, 32, 0, time.UTC), *result.Observation.ExpiresAt)
	require.Len(t, result.Observation.Files, 1)
	assert.Equal(t, in.RemoteFile{FileID: "0", Path: "debian-mac-13.7.0-amd64-netinst/debian-mac-13.7.0-amd64-netinst.iso", SizeBytes: 793772032}, result.Observation.Files[0])

	create := h.server.Requests()[0]
	assert.Equal(t, "Bearer "+apiKey, create.Header.Get("Authorization"))
	assert.Equal(t, "debroxy-test/1", create.Header.Get("User-Agent"))
	assert.Contains(t, string(create.Body), `name="magnet"`)
	assert.Contains(t, string(create.Body), magnet)
	assert.Contains(t, string(create.Body), `name="allow_zip"`)
}

func TestCreateRemoteItem_cached_torrent_file(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "25-create-sintel-torrentfile.http"), fixture(t, "25-sintel-mylist.http"))

	result, err := h.gateway.CreateRemoteItem(context.Background(),
		in.Source{Type: in.SourceTorrentFile, File: []byte("d4:infod4:name6:Sinteleee"), FileName: "sintel.torrent"}, true)

	require.NoError(t, err)
	assert.Equal(t, in.StateReady, result.Observation.State, "download_state cached with the download present")
	assert.Len(t, result.Observation.Files, 11)
	assert.Contains(t, result.Observation.Files, in.RemoteFile{FileID: "10", Path: "Sintel/Sintel.mp4", SizeBytes: 129241752})
	assert.Equal(t, &in.Bundle{Format: in.BundleZip, RangeSupported: false}, result.Observation.Bundle)
	assert.Contains(t, string(h.server.Requests()[0].Body), `filename="sintel.torrent"`)
}

func TestCreateRemoteItem_reports_queued_when_the_look_up_fails(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "02-create-magnet-cached.http"), fixture(t, "09-mylist-id-after-delete.http"))

	result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: "magnet:?xt=urn:btih:7acf"}, true)

	require.NoError(t, err)
	assert.Equal(t, in.Observation{ProviderItemID: "116409037", Kind: in.KindTorrent, ProviderHash: "7acf8fb590b2060dd9c3146ef770169d593433b0", State: in.StateQueued}, result.Observation)
}

func TestCreateRemoteItem_provider_dedup_returns_the_same_id(t *testing.T) {
	t.Parallel()
	notFound := fixture(t, "09-mylist-id-after-delete.http")

	for _, name := range []string{"03-readd-same-magnet.http", "03-readd-uppercase-bare-magnet.http", "03-add-same-as-torrentfile.http", "30-readd-after-delete-1-create.http", "30-readd-after-delete-3-recreate.http", "12-create-magnet-for-webhook.http", "21-create-torrentfile-soundie13.http", "05-create-torrentfile-multifile.http"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			created := fixture(t, name)
			var data struct {
				Data struct {
					TorrentID int64 `json:"torrent_id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(created.Body, &data))
			lookup := notFound.WithRequest(http.MethodGet, fmt.Sprintf("https://api.torbox.app/v1/api/torrents/mylist?id=%d&bypass_cache=true", data.Data.TorrentID))
			h := newHarness(t, created, lookup)

			result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: "magnet:?xt=urn:btih:x"}, false)

			require.NoError(t, err)
			assert.Equal(t, fmt.Sprint(data.Data.TorrentID), result.ProviderItemID)
		})
	}
}

func TestCreateRemoteItem_web_download(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ create, poll, id string }{
		{"05-create-webdl-archive-item.http", "05-poll-webdl-item-04.http", "1975283"},
		{"08-create-webdl-archive-org.http", "08-poll-webdl-01.http", "1975274"},
		{"08-readd-same-webdl.http", "08-poll-webdl-01.http", "1975274"},
	} {
		t.Run(tc.create, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, fixture(t, tc.create), fixture(t, tc.poll))

			result, err := h.gateway.CreateRemoteItem(context.Background(),
				in.Source{Type: in.SourceWebURL, URL: "https://archive.org/details/AvengeDe1942", Password: "hoster-pass"}, false)

			require.NoError(t, err)
			assert.Equal(t, tc.id, result.ProviderItemID)
			assert.Equal(t, in.KindWeb, result.Observation.Kind)
			assert.Nil(t, result.Observation.Bundle, "no zip for web items (ADR 0011)")
			body := string(h.server.Requests()[0].Body)
			assert.Contains(t, body, `name="link"`)
			assert.Contains(t, body, `name="password"`)
		})
	}

	t.Run("webhook_create", func(t *testing.T) {
		t.Parallel()
		lookup := fixture(t, "09-mylist-id-after-delete.http").WithRequest(http.MethodGet, "https://api.torbox.app/v1/api/webdl/mylist?id=1975505&bypass_cache=true")
		h := newHarness(t, fixture(t, "12-create-webdl-for-webhook.http"), lookup)

		result, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceWebURL, URL: "https://archive.org/x"}, false)

		require.NoError(t, err)
		assert.Equal(t, "1975505", result.ProviderItemID)
	})
}

func TestCreateRemoteItem_definite_failures(t *testing.T) {
	t.Parallel()
	webCreate := "https://api.torbox.app/v1/api/webdl/createwebdownload"

	testCases := []struct {
		name     string
		exchange replay.Exchange
		want     error
	}{
		{"unsupported_site", fixture(t, "08-create-webdl-debian-iso.http"), in.UnsupportedHosterError{Provider: "torbox"}},
		{"plan_restricted", fixture(t, "07-create-nzb-debian10.http").WithRequest(http.MethodPost, webCreate), out.ErrPlanRestricted},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tc.exchange)

			_, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceWebURL, URL: "https://example.org/x"}, false)

			assert.ErrorIs(t, err, tc.want)
			assert.Len(t, h.server.Requests(), 1, "creates are never retried")
		})
	}
}

func TestCreateRemoteItem_ambiguous_failures(t *testing.T) {
	t.Parallel()
	torrentCreate := "https://api.torbox.app/v1/api/torrents/createtorrent"
	rateLimited := fixture(t, "02-create-magnet-uncached.http")
	rateLimited.Status = http.StatusTooManyRequests
	serverError := fixture(t, "09-delete-torrent-again.http").WithRequest(http.MethodPost, torrentCreate)

	for name, exchange := range map[string]replay.Exchange{"429": rateLimited, "500_database_error": serverError} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, exchange)

			_, err := h.gateway.CreateRemoteItem(context.Background(), in.Source{Type: in.SourceMagnet, Magnet: "magnet:?x"}, false)

			var ambiguous in.AmbiguousCreateError
			assert.ErrorAs(t, err, &ambiguous)
			assert.Len(t, h.server.Requests(), 1)
		})
	}
}

func TestCreateRemoteItem_budget_block_sends_nothing(t *testing.T) {
	t.Parallel()
	created := fixture(t, "02-create-magnet-uncached.http")
	created.Header = http.Header{} // no rate headers: the static budget decides
	lookup := fixture(t, "02-poll-uncached-01.http")
	h := newHarnessWithBudgets(t, map[string]torbox.RateBudget{
		"general":        {Requests: 300, Per: time.Minute},
		"uncachedCreate": {Requests: 1, Per: time.Hour},
		"cdnZip":         {Requests: 60, Per: time.Minute},
	}, created, lookup)
	source := in.Source{Type: in.SourceMagnet, Magnet: "magnet:?x"}

	_, err := h.gateway.CreateRemoteItem(context.Background(), source, false)
	require.NoError(t, err)
	_, err = h.gateway.CreateRemoteItem(context.Background(), source, false)

	var notSent in.CreateNotSentError
	require.ErrorAs(t, err, &notSent)
	assert.Equal(t, time.Hour, notSent.RetryAfter)
	assert.Len(t, h.server.Requests(), 2, "the blocked create never reached the provider")
}

func TestDescribeRemoteItem_maps_every_observed_state(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		fixture string
		kind    in.Kind
		id      string
		state   in.State
		stalled bool
		files   int
	}{
		{"05-poll-multifile-01.http", in.KindTorrent, "116409459", in.StateDownloading, true, 0},
		{"05-poll-multifile-02.http", in.KindTorrent, "116409459", in.StateDownloading, false, 0},
		{"05-poll-multifile-03.http", in.KindTorrent, "116409459", in.StateDownloading, true, 0},
		{"02-poll-uncached-01.http", in.KindTorrent, "116409053", in.StateReady, false, 1},
		{"25-sintel-mylist.http", in.KindTorrent, "116446015", in.StateReady, false, 11},
		{"05-poll-webdl-item-01.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-02.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-03.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-05.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-06.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-07.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-08.http", in.KindWeb, "1975283", in.StateDownloading, false, 0},
		{"05-poll-webdl-item-09.http", in.KindWeb, "1975283", in.StateProcessing, false, 0},
		{"05-poll-webdl-item-10.http", in.KindWeb, "1975283", in.StateReady, false, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, fixture(t, tc.fixture))

			observation, err := h.gateway.DescribeRemoteItem(context.Background(), tc.kind, tc.id)

			require.NoError(t, err)
			assert.Equal(t, tc.state, observation.State)
			assert.Equal(t, tc.stalled, observation.Telemetry.Stalled)
			assert.Len(t, observation.Files, tc.files)
			assert.True(t, observation.ObservedAt.IsZero(), "the operation stamps the time")
		})
	}
}

func TestDescribeRemoteItem_unknown_state_is_kept_and_logged_once(t *testing.T) {
	t.Parallel()
	body := bytes.Replace(fixture(t, "05-poll-multifile-02.http").Body, []byte(`"download_state":"downloading"`), []byte(`"download_state":"frobnicating"`), 1)
	h := newHarness(t, fixture(t, "05-poll-multifile-02.http").WithBody(body), fixture(t, "05-poll-multifile-02.http").WithBody(body))

	for range 2 {
		observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "116409459")
		require.NoError(t, err)
		assert.Equal(t, in.StateUnknown, observation.State)
	}
	assert.Equal(t, 1, strings.Count(h.logs.String(), "frobnicating"))
}

// TorBox seeds a finished torrent and reports it as uploading (TB-N15):
// a finished, present download is ready whatever the state says.
func TestDescribeRemoteItem_seeding_after_completion_is_ready(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"uploading", "uploading (no peers)"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			body := bytes.Replace(fixture(t, "25-sintel-mylist.http").Body, []byte(`"download_state":"cached"`), []byte(`"download_state":"`+state+`"`), 1)
			h := newHarness(t, fixture(t, "25-sintel-mylist.http").WithBody(body))

			observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "116446015")

			require.NoError(t, err)
			assert.Equal(t, in.StateReady, observation.State)
			assert.Len(t, observation.Files, 11)
			assert.NotContains(t, h.logs.String(), "unknown TorBox download_state")
		})
	}
}

func TestDescribeRemoteItem_uploading_before_completion_is_processing(t *testing.T) {
	t.Parallel()
	body := bytes.Replace(fixture(t, "05-poll-multifile-02.http").Body, []byte(`"download_state":"downloading"`), []byte(`"download_state":"uploading (no peers)"`), 1)
	h := newHarness(t, fixture(t, "05-poll-multifile-02.http").WithBody(body))

	observation, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "116409459")

	require.NoError(t, err)
	assert.Equal(t, in.StateProcessing, observation.State)
	assert.NotContains(t, h.logs.String(), "unknown TorBox download_state")
}

func TestDescribeRemoteItem_not_found(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "09-mylist-id-after-delete.http"))

	_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "116409037")

	assert.ErrorIs(t, err, in.ErrRemoteItemNotFound)
}

func TestDeleteRemoteItem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fixture string
		kind    in.Kind
		id      string
		body    string
	}{
		{"09-delete-torrent-116409037.http", in.KindTorrent, "116409037", `{"operation":"delete","torrent_id":116409037}`},
		{"09-delete-torrent-116409053.http", in.KindTorrent, "116409053", `{"operation":"delete","torrent_id":116409053}`},
		{"09-delete-torrent-116409459.http", in.KindTorrent, "116409459", `{"operation":"delete","torrent_id":116409459}`},
		{"09-delete-torrent-116426649.http", in.KindTorrent, "116426649", `{"operation":"delete","torrent_id":116426649}`},
		{"28-delete-sintel.http", in.KindTorrent, "116446015", `{"operation":"delete","torrent_id":116446015}`},
		{"28-delete-soundie13.http", in.KindTorrent, "116439449", `{"operation":"delete","torrent_id":116439449}`},
		{"30-readd-after-delete-2-delete.http", in.KindTorrent, "116601526", `{"operation":"delete","torrent_id":116601526}`},
		{"30-readd-after-delete-4-cleanup.http", in.KindTorrent, "116601554", `{"operation":"delete","torrent_id":116601554}`},
		{"09-delete-webdl-1975274.http", in.KindWeb, "1975274", `{"operation":"delete","webdl_id":1975274}`},
		{"09-delete-webdl-1975283.http", in.KindWeb, "1975283", `{"operation":"delete","webdl_id":1975283}`},
		{"09-delete-webdl-1975505.http", in.KindWeb, "1975505", `{"operation":"delete","webdl_id":1975505}`},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, fixture(t, tc.fixture))

			err := h.gateway.DeleteRemoteItem(context.Background(), tc.kind, tc.id)

			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(h.server.Requests()[0].Body))
		})
	}
}

func TestDeleteRemoteItem_second_delete_is_an_error_with_swapped_fields(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "09-delete-torrent-again.http"))

	err := h.gateway.DeleteRemoteItem(context.Background(), in.KindTorrent, "116409037")

	var unavailable in.ProviderUnavailableError
	require.ErrorAs(t, err, &unavailable, "DATABASE_ERROR arrives in detail; the operation then verifies with a look-up")
	assert.Equal(t, "provider database error", unavailable.Cause)
}

func TestCheckCached(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "06-checkcached-object-listfiles.http"))
	hashes := []string{"7acf8fb590b2060dd9c3146ef770169d593433b0", "986fd1910cfe8d20052fe3ee49f5ded6acf0015a", "0123456789abcdef0123456789abcdef01234567"}

	result, err := h.gateway.CheckCached(context.Background(), in.KindTorrent, hashes)

	require.NoError(t, err)
	require.Len(t, result.Items, 3)
	assert.True(t, result.Items[0].Cached)
	assert.Equal(t, []in.RemoteFile{{FileID: "0", Path: "debian-13.7.0-amd64-netinst/debian-13.7.0-amd64-netinst.iso", SizeBytes: 792723456}}, result.Items[0].Files)
	assert.False(t, result.Items[1].Cached, "uncached hashes are absent from the answer")
	assert.False(t, result.Items[2].Cached)
}

func TestCheckCached_other_recordings(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"06-checkcached-others.http", "23-checkcached-webtorrent.http", "06-checkcached-list.http"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			exchange := fixture(t, name)
			hashes := strings.Split(exchange.URL.Query().Get("hash"), ",")
			// The expected answer is whatever the provider recorded as cached.
			var env struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(exchange.Body, &env))
			wantCached := map[string]bool{}
			var asObject map[string]json.RawMessage
			if json.Unmarshal(env.Data, &asObject) == nil {
				for hash := range asObject {
					wantCached[hash] = true
				}
			}
			if exchange.URL.Query().Get("format") == "list" || exchange.URL.Query().Get("list_files") == "" {
				// Recorded with other parameters than the adapter sends; replay the answer for the adapter's request.
				exchange = exchange.WithRequest(http.MethodGet, "https://api.torbox.app/v1/api/torrents/checkcached?hash="+strings.Join(hashes, ",")+"&format=object&list_files=true")
			}
			h := newHarness(t, exchange)

			result, err := h.gateway.CheckCached(context.Background(), in.KindTorrent, hashes)

			require.NoError(t, err)
			require.Len(t, result.Items, len(hashes))
			for _, item := range result.Items {
				assert.Equal(t, wantCached[item.ProviderHash], item.Cached, item.ProviderHash)
			}
		})
	}
}

func TestHosterCatalogue_is_cached_for_an_hour(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "08-webdl-hosters.http"))

	catalogue, err := h.gateway.HosterCatalogue(context.Background())
	require.NoError(t, err)
	again, err := h.gateway.HosterCatalogue(context.Background())
	require.NoError(t, err)

	assert.Greater(t, len(catalogue.Hosters), 100)
	assert.Equal(t, catalogue, again, "served from the cache: the server holds only one answer")
	assert.Equal(t, "Rapidgator", catalogue.Hosters[0].Name)
	assert.Equal(t, []string{"rapidgator.net", "rg.to"}, catalogue.Hosters[0].Domains)
	assert.False(t, catalogue.Hosters[0].Up)
	require.NotNil(t, catalogue.Hosters[0].DailyLimit)
	assert.Equal(t, int64(5), *catalogue.Hosters[0].DailyLimit)
}

// page asks for a list transcript with the adapter's paging parameters;
// the transcripts were recorded without them.
func page(e replay.Exchange, limit, offset int) replay.Exchange {
	u := *e.URL
	query := u.Query()
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	u.RawQuery = query.Encode()
	return e.WithRequest(e.Method, u.String())
}

func TestListRemoteItems_reads_every_kind_and_the_queue(t *testing.T) {
	t.Parallel()
	torrents := page(fixture(t, "02-baseline-torrents-mylist.http").WithBody(listBody(t, itemData(t, "25-sintel-mylist.http"), itemData(t, "05-poll-multifile-03.http"))), 1000, 0)
	webs := page(fixture(t, "02-baseline-webdl-mylist.http").WithBody(listBody(t, itemData(t, "05-poll-webdl-item-10.http"))), 1000, 0)
	h := newHarness(t, torrents, webs, fixture(t, "02-baseline-queued.http"))

	listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent, in.KindWeb})

	require.NoError(t, err)
	assert.True(t, listing.Complete)
	require.Len(t, listing.Observations, 3)
	assert.Equal(t, "116446015", listing.Observations[0].ProviderItemID)
	assert.Equal(t, in.KindWeb, listing.Observations[2].Kind)
}

func TestListRemoteItems_walks_the_pages(t *testing.T) {
	t.Parallel()
	list := fixture(t, "02-baseline-torrents-mylist.http")
	sintel, multi := itemData(t, "25-sintel-mylist.http"), itemData(t, "05-poll-multifile-03.http")
	h := newHarnessWithPageSize(t, 1,
		page(list.WithBody(listBody(t, sintel)), 1, 0), page(list.WithBody(listBody(t, multi)), 1, 1), page(list.WithBody(listBody(t)), 1, 2),
		fixture(t, "02-baseline-queued.http"))

	listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent})

	require.NoError(t, err)
	assert.True(t, listing.Complete)
	assert.Len(t, listing.Observations, 2, "TorBox pages its lists (limit and offset, checked live)")
}

func TestListRemoteItems_empty_account(t *testing.T) {
	t.Parallel()

	for _, set := range [][]string{
		{"09-final-torrents-mylist.http", "09-final-webdl-mylist.http", "09-final-queued.http"},
		{"28-final-torrents.http", "28-final-webdl.http", "09-final-queued.http"},
		{"30-readd-after-delete-5-final.http", "28-final-webdl.http", "09-final-queued.http"},
	} {
		t.Run(set[0], func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, page(fixture(t, set[0]), 1000, 0), page(fixture(t, set[1]), 1000, 0), fixture(t, set[2]))

			listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent, in.KindWeb})

			require.NoError(t, err)
			assert.True(t, listing.Complete)
			assert.Empty(t, listing.Observations)
		})
	}
}

func TestListRemoteItems_a_failed_list_makes_it_incomplete(t *testing.T) {
	t.Parallel()
	failedTorrents := fixture(t, "09-delete-torrent-again.http").WithRequest(http.MethodGet, "https://api.torbox.app/v1/api/torrents/mylist?bypass_cache=true&limit=1000&offset=0")
	webs := page(fixture(t, "02-baseline-webdl-mylist.http").WithBody(listBody(t, itemData(t, "05-poll-webdl-item-10.http"))), 1000, 0)
	h := newHarness(t, failedTorrents, webs, fixture(t, "02-baseline-queued.http"))

	listing, err := h.gateway.ListRemoteItems(context.Background(), []in.Kind{in.KindTorrent, in.KindWeb})

	require.NoError(t, err)
	assert.False(t, listing.Complete)
	assert.Len(t, listing.Observations, 1)
}

func TestErrors_without_an_envelope(t *testing.T) {
	t.Parallel()
	describe := "https://api.torbox.app/v1/api/torrents/mylist?id=1&bypass_cache=true"

	testCases := []struct {
		fixture string
		want    error
		logged  string
	}{
		{"01-auth-invalid-key.http", in.ProviderUnavailableError{Provider: "torbox", Cause: "credentials rejected"}, "operator_alert"},
		{"01-auth-missing.http", in.ProviderUnavailableError{Provider: "torbox", Cause: "authentication refused"}, "operator_alert"},
		{"04-requestdl-header-only.http", in.ProviderRejectedError{Provider: "torbox", Detail: "request rejected as malformed"}, "adapter bug"},
	}

	for _, tc := range testCases {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, fixture(t, tc.fixture).WithRequest(http.MethodGet, describe))

			_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "1")

			assert.Equal(t, tc.want, err)
			assert.Contains(t, h.logs.String(), tc.logged)
		})
	}
}

func TestTranslateWebhook(t *testing.T) {
	t.Parallel()
	var received []struct {
		Body string `json:"body"`
	}
	require.NoError(t, json.Unmarshal(fixtureBody(t, "12-webhook-received.json"), &received))
	h := newHarness(t)

	signal, err := h.gateway.TranslateWebhook(nil, []byte(received[0].Body))
	require.NoError(t, err)
	assert.True(t, signal.RefreshRequested)

	_, err = h.gateway.TranslateWebhook(nil, []byte("not json"))
	assert.ErrorIs(t, err, in.ErrWebhookRejected)
}

func TestErrors_never_contain_the_api_key(t *testing.T) {
	t.Parallel()
	h := newHarness(t, fixture(t, "01-auth-invalid-key.http").WithRequest(http.MethodGet, "https://api.torbox.app/v1/api/torrents/mylist?id=1&bypass_cache=true"))

	_, err := h.gateway.DescribeRemoteItem(context.Background(), in.KindTorrent, "1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), apiKey)

	// A transport failure names the host only.
	dead, err := torbox.New(torbox.Config{
		AccountID: "torbox", BaseURL: "http://127.0.0.1:1/v1/api", APIKey: apiKey, UserAgent: "t", Timeout: time.Second,
		Retry: httpclient.RetryConfig{MaxAttempts: 1}, CircuitBreaker: httpclient.CircuitBreakerConfig{FailureThreshold: 5, OpenDuration: time.Minute},
		Egress:          httpclient.Egress{Family: httpclient.FamilyIPv4},
		RateBudgets:     map[string]torbox.RateBudget{"general": {Requests: 1, Per: time.Minute}, "uncachedCreate": {Requests: 1, Per: time.Hour}, "cdnZip": {Requests: 1, Per: time.Minute}},
		CDNLinkCacheMax: time.Minute,
		Clock:           clock.NewFixed(time.Now()), Logger: slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	_, err = dead.DescribeRemoteItem(context.Background(), in.KindTorrent, "1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), apiKey)
}
