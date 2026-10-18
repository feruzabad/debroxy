package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/cmd/debroxy/config"
	"github.com/feruzabad/debroxy/internal/common/sqlite"
)

const secret32 = "0123456789abcdef0123456789abcdef"

// testEnviron is a valid environment whose startup egress check dials a
// local listener instead of the real TorBox hosts; extra entries are
// appended (later entries win, as in a process environment).
func testEnviron(t *testing.T, extra ...string) (environ []string, databasePath string) {
	t.Helper()
	dir := t.TempDir()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	local := listener.Addr().String()
	environ = append([]string{
		"DEBROXY_DATA_DIR=" + dir,
		"DEBROXY_SERVER_LISTEN=127.0.0.1:0",
		"DEBROXY_SERVER_PUBLIC_BASE_URL=https://debroxy.example.org",
		"DEBROXY_SERVER_SHUTDOWN_TIMEOUT=2s",
		"DEBROXY_LOG_LEVEL=debug",
		"DEBROXY_SIGNING_KEYS=k1",
		"DEBROXY_SIGNING_KEY_K1=" + secret32,
		"DEBROXY_PROVIDERS=torbox",
		"DEBROXY_PROVIDER_TORBOX_KIND=torbox",
		"DEBROXY_PROVIDER_TORBOX_API_KEY=torbox-key",
		"DEBROXY_PROVIDER_TORBOX_BASE_URL=https://" + local + "/v1/api",
		"DEBROXY_PROVIDER_TORBOX_CDN_PROBE_HOSTS=" + local,
	}, extra...)
	return environ, filepath.Join(dir, "debroxy.db")
}

func mustConfig(t *testing.T, environ []string) config.Config {
	t.Helper()
	cfg, err := config.FromEnv(environ)
	require.NoError(t, err)
	return cfg
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { assert.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	return response.StatusCode, decoded
}

func TestServe_starts_answers_probes_and_shuts_down_cleanly(t *testing.T) {
	t.Parallel()
	environ, databasePath := testEnviron(t)
	cfg := mustConfig(t, environ)
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	process, err := startProcess(ctx, cfg, newLogger(cfg.Log, &logs))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process.run(ctx) }()
	baseURL := "http://" + process.listener.Addr().String()

	status, body := get(t, baseURL+"/healthz")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "alive", body["status"])
	status, body = get(t, baseURL+"/readyz")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ready", body["status"])
	status, body = get(t, baseURL+"/api/v1/jobs")
	assert.Equal(t, http.StatusUnauthorized, status, "job is mounted and needs a session")
	assert.Equal(t, "authentication-required", body["slug"])
	status, body = get(t, baseURL+"/api/v1/admin/users")
	assert.Equal(t, http.StatusUnauthorized, status, "identity is mounted and needs a session")
	assert.Equal(t, "authentication-required", body["slug"])

	_, err = sqlite.Open(ctx, sqlite.Config{Path: databasePath})
	var lockedErr sqlite.DatabaseLockedError
	assert.ErrorAs(t, err, &lockedErr, "the running process holds the start lock")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}

	reopened, err := sqlite.Open(context.Background(), sqlite.Config{Path: databasePath})
	require.NoError(t, err, "shutdown releases the start lock")
	require.NoError(t, reopened.Close())
	assert.Contains(t, logs.String(), `"msg":"debroxy started"`)
	assert.Contains(t, logs.String(), `"msg":"debroxy stopped"`)
	assert.Contains(t, logs.String(), `"request_id"`, "access log lines carry the request ID")
	assert.NotContains(t, logs.String(), "torbox-key")
	assert.NotContains(t, logs.String(), secret32)
}

func TestRun_migrate_creates_the_database(t *testing.T) {
	t.Parallel()
	environ, databasePath := testEnviron(t)
	var stderr bytes.Buffer

	err := run(context.Background(), []string{"migrate"}, strings.NewReader(""), &stderr, environ)

	require.NoError(t, err)
	assert.FileExists(t, databasePath)
	assert.Contains(t, stderr.String(), "migrations applied")
}

func TestRun_rejects_bad_command_lines(t *testing.T) {
	t.Parallel()
	environ, _ := testEnviron(t, "DEBROXY_PROVIDER_TORBOX_API_KEY=")

	testCases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no_command", nil, "usage: debroxy"},
		{"unknown_command", []string{"start"}, `unknown command "start"`},
		{"removed_config_flag", []string{"serve", "--config", "x.yaml"}, "flag provided but not defined: -config"},
		{"invalid_config", []string{"serve"}, "DEBROXY_PROVIDER_TORBOX_API_KEY: must be set"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := run(context.Background(), tc.args, strings.NewReader(""), io.Discard, environ)

			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantErr), err.Error())
		})
	}
}

func TestRun_bootstrap_admin_then_login(t *testing.T) {
	t.Parallel()
	environ, _ := testEnviron(t)
	var stderr bytes.Buffer

	err := run(context.Background(), []string{"bootstrap-admin", "--username", "root"},
		strings.NewReader("correct horse battery\n"), &stderr, environ)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "first admin created")
	assert.NotContains(t, stderr.String(), "correct horse battery")

	err = run(context.Background(), []string{"bootstrap-admin", "--username", "second"},
		strings.NewReader("correct horse battery\n"), io.Discard, environ)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "users already exist")

	cfg := mustConfig(t, environ)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := startProcess(ctx, cfg, newLogger(cfg.Log, io.Discard))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process.run(ctx) }()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+process.listener.Addr().String()+"/api/v1/auth/login",
		strings.NewReader(`{"username":"root","password":"correct horse battery"}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusOK, response.StatusCode)

	cancel()
	require.NoError(t, <-done)
}

func TestServe_bootstraps_the_first_admin_from_the_environment(t *testing.T) {
	t.Parallel()
	environ, _ := testEnviron(t,
		"DEBROXY_BOOTSTRAP_ADMIN_USERNAME=root", "DEBROXY_BOOTSTRAP_ADMIN_PASSWORD="+adminSecret)
	cfg := mustConfig(t, environ)

	for _, want := range []string{"first admin created", "first admin bootstrap skipped: users already exist"} {
		logs := &syncBuffer{}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- serve(ctx, cfg, newLogger(cfg.Log, logs)) }()
		require.Eventually(t, func() bool { return strings.Contains(logs.String(), `"msg":"debroxy started"`) },
			5*time.Second, 10*time.Millisecond, logs.String())
		cancel()
		require.NoError(t, <-done)
		assert.Contains(t, logs.String(), want, "a restart on the same data directory skips the bootstrap")
		assert.NotContains(t, logs.String(), adminSecret)
	}
}

func TestRun_healthcheck_reports_readiness(t *testing.T) {
	t.Parallel()
	environ, _ := testEnviron(t)
	cfg := mustConfig(t, environ)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := startProcess(ctx, cfg, newLogger(cfg.Log, io.Discard))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- process.run(ctx) }()
	listen := "DEBROXY_SERVER_LISTEN=" + process.listener.Addr().String()

	assert.NoError(t, run(ctx, []string{"healthcheck"}, nil, io.Discard, []string{listen}), "needs no other configuration")

	cancel()
	require.NoError(t, <-done)
	assert.Error(t, run(context.Background(), []string{"healthcheck"}, nil, io.Discard, []string{listen}), "nothing listens any more")
}
