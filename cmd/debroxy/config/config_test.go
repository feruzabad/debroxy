package config_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/cmd/debroxy/config"
)

const secret32 = "0123456789abcdef0123456789abcdef"

// minimalEnv is the smallest valid environment; dataDir must exist.
func minimalEnv(dataDir string) map[string]string {
	return map[string]string{
		"DEBROXY_DATA_DIR":                dataDir,
		"DEBROXY_SERVER_PUBLIC_BASE_URL":  "https://debroxy.example.org",
		"DEBROXY_SIGNING_KEYS":            "k1",
		"DEBROXY_SIGNING_KEY_K1":          secret32,
		"DEBROXY_PROVIDERS":               "torbox",
		"DEBROXY_PROVIDER_TORBOX_KIND":    "torbox",
		"DEBROXY_PROVIDER_TORBOX_API_KEY": "torbox-key",
	}
}

func environ(values map[string]string) []string {
	entries := make([]string, 0, len(values))
	for name, value := range values {
		entries = append(entries, name+"="+value)
	}
	return entries
}

// with returns base with changes applied; an empty value deletes the variable.
func with(base map[string]string, changes map[string]string) map[string]string {
	merged := maps.Clone(base)
	for name, value := range changes {
		if value == "" {
			delete(merged, name)
			continue
		}
		merged[name] = value
	}
	return merged
}

func TestFromEnv_applies_defaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cfg, err := config.FromEnv(environ(minimalEnv(dir)))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "debroxy.db"), cfg.DatabasePath())
	assert.Equal(t, ":8080", cfg.Server.Listen)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, 3, cfg.Job.DefaultSlotQuota)
	assert.Equal(t, 20*time.Minute, cfg.Job.SubmissionTimeout.Std())
	assert.Equal(t, 64, cfg.Delivery.MaxConcurrentStreams)
	assert.Equal(t, "https://debroxy.example.org", cfg.Server.PublicURL.String())
	require.Len(t, cfg.Delivery.SigningKeys, 1)
	assert.Equal(t, "k1", cfg.Delivery.SigningKeys[0].ID)

	torbox := cfg.Providers[0]
	assert.Equal(t, "torbox", torbox.ID)
	assert.Equal(t, "torbox-key", torbox.APIKey.Reveal())
	assert.True(t, torbox.IsEnabled())
	assert.Equal(t, "TorBox", torbox.DisplayName)
	assert.Equal(t, "https://api.torbox.app/v1/api", torbox.BaseURL)
	assert.Equal(t, "debroxy/0.1", torbox.UserAgent)
	assert.Equal(t, "ipv4", torbox.Egress.IPFamily)
	assert.Equal(t, config.RateBudget{Requests: 60, Per: config.Duration(time.Hour)}, torbox.RateBudgets["uncachedCreate"])
}

func TestFromEnv_reads_every_value_type(t *testing.T) {
	t.Parallel()
	env := with(minimalEnv(t.TempDir()), map[string]string{
		"DEBROXY_PROVIDERS":                                     "torbox, rd-2",
		"DEBROXY_PROVIDER_RD_2_KIND":                            "realdebrid",
		"DEBROXY_PROVIDER_RD_2_API_KEY":                         "rd-key",
		"DEBROXY_PROVIDER_RD_2_ENABLED":                         "false",
		"DEBROXY_PROVIDER_RD_2_ABSENCE_GRACE":                   "2m",
		"DEBROXY_SERVER_TRUSTED_PROXIES":                        "10.0.0.0/8, 192.168.1.1",
		"DEBROXY_JOB_UPLOAD_LIMIT_BYTES":                        "1024",
		"DEBROXY_JOB_FOLLOW_UP_REFRESHES":                       "10s,1m",
		"DEBROXY_PROVIDER_TORBOX_CDN_PROBE_HOSTS":               "a.example,b.example:8443",
		"DEBROXY_PROVIDER_TORBOX_RATE_BUDGET_GENERAL":           "100/60s",
		"DEBROXY_PROVIDER_TORBOX_RATE_BUDGET_CDN_ZIP":           "5/1m",
		"DEBROXY_PROVIDER_TORBOX_WEBHOOK_ENABLED":               "true",
		"DEBROXY_PROVIDER_TORBOX_WEBHOOK_SECRET":                secret32,
		"DEBROXY_PROVIDER_TORBOX_EGRESS_LOCAL_ADDRESS":          "192.0.2.10",
		"DEBROXY_PROVIDER_TORBOX_CIRCUIT_BREAKER_OPEN_DURATION": "45s",
	})

	cfg, err := config.FromEnv(environ(env))

	require.NoError(t, err)
	assert.Len(t, cfg.Server.TrustedProxyRange, 2)
	assert.EqualValues(t, 1024, cfg.Job.UploadLimitBytes)
	assert.Equal(t, []config.Duration{config.Duration(10 * time.Second), config.Duration(time.Minute)}, cfg.Job.FollowUpRefreshes)
	torbox, rd := cfg.Providers[0], cfg.Providers[1]
	assert.Equal(t, []string{"a.example", "b.example:8443"}, torbox.CDNProbeHosts)
	assert.Equal(t, 100, torbox.RateBudgets["general"].Requests)
	assert.Equal(t, 5, torbox.RateBudgets["cdnZip"].Requests)
	assert.Equal(t, 60, torbox.RateBudgets["uncachedCreate"].Requests, "unset budgets keep the kind default")
	assert.True(t, torbox.Webhook.Enabled)
	assert.Equal(t, "192.0.2.10", torbox.Egress.LocalAddress)
	assert.Equal(t, 45*time.Second, torbox.CircuitBreaker.OpenDuration.Std())
	assert.Equal(t, "rd-2", rd.ID)
	assert.False(t, rd.IsEnabled())
	require.NotNil(t, rd.AbsenceGrace)
	assert.Equal(t, 2*time.Minute, rd.AbsenceGrace.Std())
}

func TestFromEnv_reads_secrets_from_files(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "api-key")
	require.NoError(t, os.WriteFile(keyFile, []byte("file-key\n"), 0o600))
	env := with(minimalEnv(dir), map[string]string{
		"DEBROXY_PROVIDER_TORBOX_API_KEY":      "",
		"DEBROXY_PROVIDER_TORBOX_API_KEY_FILE": keyFile,
	})

	cfg, err := config.FromEnv(environ(env))

	require.NoError(t, err)
	assert.Equal(t, "file-key", cfg.Providers[0].APIKey.Reveal(), "the trailing newline is removed")
}

func TestFromEnv_bootstrap_admin(t *testing.T) {
	t.Parallel()
	env := with(minimalEnv(t.TempDir()), map[string]string{
		"DEBROXY_BOOTSTRAP_ADMIN_USERNAME": "root",
		"DEBROXY_BOOTSTRAP_ADMIN_PASSWORD": "correct horse battery staple",
	})

	cfg, err := config.FromEnv(environ(env))

	require.NoError(t, err)
	assert.Equal(t, "root", cfg.Bootstrap.Username)
	assert.Equal(t, "correct horse battery staple", cfg.Bootstrap.Password.Reveal())
}

func TestFromEnv_rejects_invalid_configs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		changes     map[string]string // applied to minimalEnv; "" deletes
		wantProblem string
	}{
		{"unknown_variable", map[string]string{"DEBROXY_SERVER_LISTN": ":9"}, "DEBROXY_SERVER_LISTN: unknown variable"},
		{"old_name", map[string]string{"DEBROXY_PROVIDER_TORBOX_CDN_DOMAINS": "x"}, "DEBROXY_PROVIDER_TORBOX_CDN_DOMAINS: unknown variable"},
		{"unlisted_provider", map[string]string{"DEBROXY_PROVIDER_RD_KIND": "realdebrid"}, "DEBROXY_PROVIDER_RD_KIND: unknown variable"},
		{"bad_duration", map[string]string{"DEBROXY_SERVER_SHUTDOWN_TIMEOUT": "soon"}, `DEBROXY_SERVER_SHUTDOWN_TIMEOUT: must be a duration like "30s", got "soon"`},
		{"bad_integer", map[string]string{"DEBROXY_JOB_DEFAULT_SLOT_QUOTA": "three"}, "DEBROXY_JOB_DEFAULT_SLOT_QUOTA: must be an integer"},
		{"bad_bool", map[string]string{"DEBROXY_IDENTITY_OIDC_ENABLED": "yes"}, "DEBROXY_IDENTITY_OIDC_ENABLED: must be true or false"},
		{"missing_data_dir", map[string]string{"DEBROXY_DATA_DIR": ""}, "DEBROXY_DATA_DIR: must be set"},
		{"data_dir_missing_on_disk", map[string]string{"DEBROXY_DATA_DIR": "/nonexistent/debroxy"}, "DEBROXY_DATA_DIR: must be an existing directory"},
		{"missing_public_url", map[string]string{"DEBROXY_SERVER_PUBLIC_BASE_URL": ""}, "DEBROXY_SERVER_PUBLIC_BASE_URL: must be set"},
		{"public_url_with_path", map[string]string{"DEBROXY_SERVER_PUBLIC_BASE_URL": "https://example.org/debroxy"}, "must not have a path"},
		{"relative_public_url", map[string]string{"DEBROXY_SERVER_PUBLIC_BASE_URL": "debroxy.example.org"}, "absolute http(s) URL"},
		{"bad_trusted_proxy", map[string]string{"DEBROXY_SERVER_TRUSTED_PROXIES": "10.0.0.0/33"}, "DEBROXY_SERVER_TRUSTED_PROXIES[0]"},
		{"bad_log_level", map[string]string{"DEBROXY_LOG_LEVEL": "loud"}, "DEBROXY_LOG_LEVEL"},
		{"missing_api_key", map[string]string{"DEBROXY_PROVIDER_TORBOX_API_KEY": ""}, "DEBROXY_PROVIDER_TORBOX_API_KEY: must be set (or DEBROXY_PROVIDER_TORBOX_API_KEY_FILE)"},
		{"secret_twice", map[string]string{"DEBROXY_PROVIDER_TORBOX_API_KEY_FILE": "/x"}, "set either DEBROXY_PROVIDER_TORBOX_API_KEY or DEBROXY_PROVIDER_TORBOX_API_KEY_FILE, not both"},
		{"secret_file_missing", map[string]string{"DEBROXY_PROVIDER_TORBOX_API_KEY": "", "DEBROXY_PROVIDER_TORBOX_API_KEY_FILE": "/nonexistent/key"}, "DEBROXY_PROVIDER_TORBOX_API_KEY_FILE: read secret file"},
		{"short_signing_key", map[string]string{"DEBROXY_SIGNING_KEY_K1": "short"}, "DEBROXY_SIGNING_KEY_K1: must hold at least 32 bytes"},
		{"no_signing_keys", map[string]string{"DEBROXY_SIGNING_KEYS": "", "DEBROXY_SIGNING_KEY_K1": ""}, "DEBROXY_SIGNING_KEYS: needs at least one key ID"},
		{"no_providers", map[string]string{"DEBROXY_PROVIDERS": "", "DEBROXY_PROVIDER_TORBOX_KIND": "", "DEBROXY_PROVIDER_TORBOX_API_KEY": ""}, "DEBROXY_PROVIDERS: needs at least one provider ID"},
		{"unknown_kind", map[string]string{"DEBROXY_PROVIDER_TORBOX_KIND": "premiumize"}, `unknown provider kind "premiumize"`},
		{"duplicate_provider", map[string]string{"DEBROXY_PROVIDERS": "torbox,torbox"}, `duplicate provider ID "torbox"`},
		{"provider_id_not_slug", map[string]string{"DEBROXY_PROVIDERS": "Tor_Box"}, "lowercase letters"},
		{"unknown_rate_budget", map[string]string{"DEBROXY_PROVIDER_TORBOX_RATE_BUDGET_CDN_FILE": "1/1s"}, "DEBROXY_PROVIDER_TORBOX_RATE_BUDGET_CDN_FILE: unknown budget"},
		{"bad_rate_budget", map[string]string{"DEBROXY_PROVIDER_TORBOX_RATE_BUDGET_GENERAL": "300 per minute"}, "must be <requests>/<duration>"},
		{"bad_egress_family", map[string]string{"DEBROXY_PROVIDER_TORBOX_EGRESS_IP_FAMILY": "both"}, "DEBROXY_PROVIDER_TORBOX_EGRESS_IP_FAMILY"},
		{"egress_address_wrong_family", map[string]string{"DEBROXY_PROVIDER_TORBOX_EGRESS_LOCAL_ADDRESS": "2001:db8::1"}, "is not an ipv4 address"},
		{"short_webhook_secret", map[string]string{"DEBROXY_PROVIDER_TORBOX_WEBHOOK_ENABLED": "true", "DEBROXY_PROVIDER_TORBOX_WEBHOOK_SECRET": "short"}, "DEBROXY_PROVIDER_TORBOX_WEBHOOK_SECRET: must hold at least 32 bytes"},
		{"realdebrid_webhook", map[string]string{"DEBROXY_PROVIDER_TORBOX_KIND": "realdebrid", "DEBROXY_PROVIDER_TORBOX_WEBHOOK_ENABLED": "true", "DEBROXY_PROVIDER_TORBOX_WEBHOOK_SECRET": secret32}, "Real-Debrid offers no webhooks"},
		{"timeout_not_after_follow_ups", map[string]string{"DEBROXY_JOB_SUBMISSION_TIMEOUT": "15m"}, "must exceed the last follow-up refresh"},
		{"follow_ups_not_ascending", map[string]string{"DEBROXY_JOB_FOLLOW_UP_REFRESHES": "2m,30s"}, "must be later than the previous follow-up"},
		{"bad_probe_host", map[string]string{"DEBROXY_PROVIDER_TORBOX_CDN_PROBE_HOSTS": "https://cdn.example"}, "DEBROXY_PROVIDER_TORBOX_CDN_PROBE_HOSTS[0]: must be a host name or host:port"},
		{"oidc_incomplete", map[string]string{"DEBROXY_IDENTITY_OIDC_ENABLED": "true", "DEBROXY_IDENTITY_OIDC_ISSUER_URL": "http://auth"}, "DEBROXY_IDENTITY_OIDC_ISSUER_URL"},
		{"bootstrap_without_password", map[string]string{"DEBROXY_BOOTSTRAP_ADMIN_USERNAME": "root"}, "DEBROXY_BOOTSTRAP_ADMIN_PASSWORD: must be set"},
		{"bootstrap_without_username", map[string]string{"DEBROXY_BOOTSTRAP_ADMIN_PASSWORD": "correct horse battery staple"}, "DEBROXY_BOOTSTRAP_ADMIN_USERNAME: must be set"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.FromEnv(environ(with(minimalEnv(t.TempDir()), tc.changes)))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantProblem)
		})
	}
}

func TestFromEnv_reports_all_problems_at_once(t *testing.T) {
	t.Parallel()

	_, err := config.FromEnv([]string{"DEBROXY_TYPO=1", "PATH=/bin"})

	var validationErr config.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.GreaterOrEqual(t, len(validationErr.Problems), 5, validationErr.Error())
	assert.NotContains(t, validationErr.Error(), "PATH", "only DEBROXY_* variables are read")
}

func TestFromEnv_never_prints_secrets_in_problems(t *testing.T) {
	t.Parallel()
	env := with(minimalEnv(t.TempDir()), map[string]string{"DEBROXY_SIGNING_KEY_K1": "short-secret-value"})

	_, err := config.FromEnv(environ(env))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "short-secret-value")
}

var exampleVariable = regexp.MustCompile(`^#?\s*(DEBROXY_[A-Z0-9_]+)=(.*)$`)

// TestEnvExample_documents_every_variable keeps .env.example complete
// (spec §11) and its uncommented lines valid.
func TestEnvExample_documents_every_variable(t *testing.T) {
	t.Parallel()
	file, err := os.Open("../../../.env.example")
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	documented := map[string]bool{}
	active := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := exampleVariable.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		documented[match[1]] = true
		if !strings.HasPrefix(scanner.Text(), "#") {
			active[match[1]] = match[2]
		}
	}
	require.NoError(t, scanner.Err())

	for _, name := range config.Variables(map[string]string{"torbox": "torbox", "realdebrid": "realdebrid"}, []string{"k1"}) {
		if strings.HasPrefix(name, "DEBROXY_PROVIDER_REALDEBRID_WEBHOOK_") {
			continue // Real-Debrid offers no webhooks
		}
		assert.True(t, documented[name], "%s is missing from .env.example", name)
	}

	active["DEBROXY_DATA_DIR"] = t.TempDir()
	cfg, err := config.FromEnv(environ(active))
	require.NoError(t, err)
	assert.Len(t, cfg.Providers, 2)
}

func TestHealthcheckURL(t *testing.T) {
	t.Parallel()
	for listen, want := range map[string]string{
		"":              "http://127.0.0.1:8080/readyz",
		":9090":         "http://127.0.0.1:9090/readyz",
		"0.0.0.0:80":    "http://127.0.0.1:80/readyz",
		"[::]:8080":     "http://[::1]:8080/readyz",
		"10.1.2.3:8080": "http://10.1.2.3:8080/readyz",
	} {
		got, err := config.HealthcheckURL([]string{"DEBROXY_SERVER_LISTEN=" + listen})
		require.NoError(t, err, listen)
		assert.Equal(t, want, got, listen)
	}
	_, err := config.HealthcheckURL([]string{"DEBROXY_SERVER_LISTEN=8080"})
	assert.Error(t, err)
}

func TestSecret_never_prints_its_value(t *testing.T) {
	t.Parallel()
	secret := config.NewSecret("hunter2-value")
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("x", "secret", secret)
	asJSON, err := json.Marshal(struct{ S config.Secret }{secret})
	require.NoError(t, err)

	for _, printed := range []string{
		fmt.Sprint(secret), fmt.Sprintf("%v %+v %#v %s", secret, secret, secret, secret),
		logs.String(), string(asJSON),
	} {
		assert.NotContains(t, printed, "hunter2")
	}
	assert.Equal(t, "hunter2-value", secret.Reveal())
}
