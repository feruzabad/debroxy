//go:build discovery

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const liveMagnet = "magnet:?xt=urn:btih:" + debianHash + "&dn=debian-13.7.0-amd64-netinst.iso"

// TestLive_smoke runs the whole process against the live accounts: submit
// the cached Debian netinst magnet, read 1 MiB through a Signed Link,
// release it, and check at the provider that nothing is left. Nothing is
// downloaded locally beyond that 1 MiB.
func TestLive_smoke(t *testing.T) {
	torboxKey, realDebridKey := os.Getenv("TORBOX_API_KEY"), os.Getenv("REALDEBRID_API_KEY")
	if torboxKey == "" || realDebridKey == "" {
		t.Skip("TORBOX_API_KEY and REALDEBRID_API_KEY are needed")
	}
	// The keys move into debroxy's variable names in memory only.
	cfg := mustConfig(t, []string{
		"DEBROXY_DATA_DIR=" + t.TempDir(),
		"DEBROXY_SERVER_LISTEN=127.0.0.1:0",
		"DEBROXY_SERVER_PUBLIC_BASE_URL=https://debroxy.example.org",
		"DEBROXY_SERVER_SHUTDOWN_TIMEOUT=5s",
		"DEBROXY_SIGNING_KEYS=k1",
		"DEBROXY_SIGNING_KEY_K1=" + secret32,
		"DEBROXY_PROVIDERS=torbox,realdebrid",
		"DEBROXY_PROVIDER_TORBOX_KIND=torbox",
		"DEBROXY_PROVIDER_TORBOX_API_KEY=" + torboxKey,
		"DEBROXY_PROVIDER_TORBOX_USER_AGENT=debroxy-discovery/0.1",
		"DEBROXY_PROVIDER_REALDEBRID_KIND=realdebrid",
		"DEBROXY_PROVIDER_REALDEBRID_API_KEY=" + realDebridKey,
		"DEBROXY_PROVIDER_REALDEBRID_USER_AGENT=debroxy-discovery/0.1",
	})
	logs := &syncBuffer{}
	logger := newLogger(cfg.Log, logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, bootstrapAdmin(ctx, cfg, logger, "root", strings.NewReader(adminSecret+"\n")))
	process, err := startProcess(ctx, cfg, logger)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process.run(ctx) }()
	stack := fullStack{t: t, baseURL: "http://" + process.listener.Addr().String()}
	status, _, body := stack.call(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "root", "password": adminSecret}, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var login struct{ Token string }
	require.NoError(t, json.Unmarshal(body, &login))
	stack.token = login.Token

	for _, provider := range []string{"torbox", "realdebrid"} {
		t.Run(provider, func(t *testing.T) {
			stack.t = t
			smokeProvider(t, stack, provider)
			key := torboxKey
			if provider == "realdebrid" {
				key = realDebridKey
			}
			assert.Eventually(t, func() bool { return !providerHolds(t, provider, key) }, 30*time.Second, 2*time.Second,
				"the provider account ends without the item")
		})
	}
	stack.t = t

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
	captured := logs.String()
	for name, secret := range map[string]string{"TorBox key": torboxKey, "Real-Debrid key": realDebridKey, "session token": login.Token} {
		assert.NotContains(t, captured, secret, "the %s leaked into the logs", name)
	}
}

func smokeProvider(t *testing.T, stack fullStack, provider string) {
	status, _, body := stack.call(http.MethodPost, "/api/v1/jobs", map[string]any{"providerId": provider, "source": map[string]string{"type": "magnet", "magnet": liveMagnet}}, nil)
	require.Contains(t, []int{http.StatusCreated, http.StatusAccepted}, status, string(body))
	var job struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	jobID := jobIDOf(t, body)
	read := func() {
		status, _, body := stack.call(http.MethodGet, "/api/v1/jobs/"+jobID, nil, nil)
		require.Equal(t, http.StatusOK, status, string(body))
		require.NoError(t, json.Unmarshal(body, &job))
	}
	read()
	released := false
	t.Cleanup(func() {
		if !released {
			stack.call(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil, nil)
		}
	})
	for deadline := time.Now().Add(90 * time.Second); job.State != "ready" && time.Now().Before(deadline); {
		time.Sleep(11 * time.Second) // past the read max age, so each read lists the account
		read()
	}
	require.Equal(t, "ready", job.State)
	require.NotEmpty(t, job.Files)

	status, _, body = stack.call(http.MethodPost, "/api/v1/jobs/"+job.ID+"/links", map[string]any{"target": map[string]string{"fileId": job.Files[0].ID}}, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var link struct{ URL string }
	require.NoError(t, json.Unmarshal(body, &link))
	linkURL, err := url.Parse(link.URL)
	require.NoError(t, err)
	readMebibyte(t, stack.baseURL+linkURL.Path)

	status, _, body = stack.call(http.MethodGet, "/api/v1/jobs", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(body), `"stale":false`, "a live listing with the item in it")
	assert.Contains(t, string(body), job.ID)

	if provider == "torbox" {
		logTorBoxPaging(t)
	}

	status, _, body = stack.call(http.MethodDelete, "/api/v1/jobs/"+job.ID, nil, nil)
	require.Equal(t, http.StatusNoContent, status, string(body))
	released = true
}

func jobIDOf(t *testing.T, body []byte) string {
	var decoded struct {
		ID  string `json:"id"`
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	if decoded.ID != "" {
		return decoded.ID
	}
	return decoded.Job.ID
}

// readMebibyte reads at most 1 MiB of the target (no local torrenting).
func readMebibyte(t *testing.T, link string) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, link, nil)
	require.NoError(t, err)
	request.Header.Set("Range", "bytes=0-1048575")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	n, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	require.NoError(t, err)
	assert.Equal(t, http.StatusPartialContent, response.StatusCode)
	assert.Equal(t, int64(1<<20), n)
}

// providerAPI calls a provider directly with the account key; errors never
// print the key.
func providerAPI(t *testing.T, rawURL, key string) (int, []byte) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("User-Agent", "debroxy-discovery/0.1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("provider call failed for %s", strings.SplitN(rawURL, "?", 2)[0])
	}
	defer func() { require.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, body
}

// providerHolds reports whether the account still lists the Debian item.
func providerHolds(t *testing.T, provider, key string) bool {
	switch provider {
	case "torbox":
		_, body := providerAPI(t, "https://api.torbox.app/v1/api/torrents/mylist?bypass_cache=true", key)
		return strings.Contains(strings.ToLower(string(body)), debianHash)
	default:
		status, body := providerAPI(t, "https://api.real-debrid.com/rest/1.0/torrents", key)
		return status == http.StatusOK && strings.Contains(strings.ToLower(string(body)), debianHash)
	}
}

// logTorBoxPaging records whether mylist honours limit and offset (an
// open question of the TorBox discovery); it asserts nothing.
func logTorBoxPaging(t *testing.T) {
	key := os.Getenv("TORBOX_API_KEY")
	count := func(query string) int {
		_, body := providerAPI(t, "https://api.torbox.app/v1/api/torrents/mylist?bypass_cache=true"+query, key)
		var env struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		return len(env.Data)
	}
	t.Logf("TorBox mylist items: plain=%d limit=1&offset=0: %d limit=1&offset=1: %d", count(""), count("&limit=1&offset=0"), count("&limit=1&offset=1"))
}
