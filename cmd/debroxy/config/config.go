// Package config is the configuration of debroxy: its schema, its
// defaults, loading from environment variables and startup validation
// (ddd-rules S-3, spec §11). Contexts never see this package; cmd maps it
// to each context's own config.
//
// Each field's env tag is one segment of its variable name: nested
// structs join their segments with "_" under the DEBROXY prefix, so
// Server.PublicBaseURL is DEBROXY_SERVER_PUBLIC_BASE_URL. Fields without
// a tag are derived or loaded by hand (providers, signing keys).
package config

import (
	"net/netip"
	"net/url"
	"path/filepath"
)

// DatabaseFile is the database's file name inside the data directory.
const DatabaseFile = "debroxy.db"

// Config is the whole configuration after loading.
type Config struct {
	// DataDir holds the database and its start lock (spec §11, C12-02).
	DataDir   string    `env:"DATA_DIR"`
	Server    Server    `env:"SERVER"`
	Log       Log       `env:"LOG"`
	Database  Database  `env:"DATABASE"`
	Identity  Identity  `env:"IDENTITY"`
	Bootstrap Bootstrap `env:"BOOTSTRAP_ADMIN"`
	Job       Job       `env:"JOB"`
	Delivery  Delivery  `env:"DELIVERY"`
	Providers []Provider
}

// DatabasePath is the database file in the data directory.
func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, DatabaseFile) }

type Server struct {
	Listen        string `env:"LISTEN"`
	PublicBaseURL string `env:"PUBLIC_BASE_URL"`
	// TrustedProxies are CIDRs or single addresses whose X-Forwarded-For is trusted.
	TrustedProxies  []string `env:"TRUSTED_PROXIES"`
	ShutdownTimeout Duration `env:"SHUTDOWN_TIMEOUT"`

	// Derived by validation.
	PublicURL         *url.URL
	TrustedProxyRange []netip.Prefix
}

type Log struct {
	// Level is one of debug, info, warn, error.
	Level string `env:"LEVEL"`
}

type Database struct {
	ReaderConnections int `env:"READER_CONNECTIONS"`
}

type Identity struct {
	Session Session `env:"SESSION"`
	OIDC    OIDC    `env:"OIDC"`
}

type Session struct {
	AbsoluteLifetime Duration `env:"ABSOLUTE_LIFETIME"`
	IdleTimeout      Duration `env:"IDLE_TIMEOUT"`
}

type OIDC struct {
	Enabled      bool     `env:"ENABLED"`
	IssuerURL    string   `env:"ISSUER_URL"`
	ClientID     string   `env:"CLIENT_ID"`
	ClientSecret Secret   `env:"CLIENT_SECRET"`
	Scopes       []string `env:"SCOPES"`
}

// Bootstrap creates the first admin when serve starts (spec §11, C12-03).
type Bootstrap struct {
	Username string `env:"USERNAME"`
	Password Secret `env:"PASSWORD"`
}

type Job struct {
	DefaultSlotQuota       int           `env:"DEFAULT_SLOT_QUOTA"`
	UploadLimitBytes       int64         `env:"UPLOAD_LIMIT_BYTES"`
	SubmitRate             SubmitRate    `env:"SUBMIT_RATE"`
	ForcedRefresh          ForcedRefresh `env:"FORCED_REFRESH"`
	RefreshMaxAge          Duration      `env:"REFRESH_MAX_AGE"`
	SafetyRefresh          SafetyRefresh `env:"SAFETY_REFRESH"`
	StallThreshold         Duration      `env:"STALL_THRESHOLD"`
	SubmissionTimeout      Duration      `env:"SUBMISSION_TIMEOUT"`
	SubmitRequestDeadline  Duration      `env:"SUBMIT_REQUEST_DEADLINE"`
	AbsenceGrace           Duration      `env:"ABSENCE_GRACE"`
	RefreshBackoff         Backoff       `env:"REFRESH_BACKOFF"`
	FollowUpRefreshes      []Duration    `env:"FOLLOW_UP_REFRESHES"`
	WebhookRefreshInterval Duration      `env:"WEBHOOK_REFRESH_INTERVAL"`
	Outbox                 Outbox        `env:"OUTBOX"`
	Retention              Retention     `env:"RETENTION"`
	Events                 Events        `env:"EVENTS"`
}

// Events limits the open Event Streams (spec §5.11).
type Events struct {
	MaxStreamsPerUser int `env:"MAX_STREAMS_PER_USER"`
	MaxStreamsTotal   int `env:"MAX_STREAMS_TOTAL"`
}

type SubmitRate struct {
	PerHour       int `env:"PER_HOUR"`
	CachedPerHour int `env:"CACHED_PER_HOUR"`
}

type ForcedRefresh struct {
	PerUserPerMinute int `env:"PER_USER_PER_MINUTE"`
}

type SafetyRefresh struct {
	Active Duration `env:"ACTIVE"`
	Idle   Duration `env:"IDLE"`
}

type Backoff struct {
	Initial Duration `env:"INITIAL"`
	Max     Duration `env:"MAX"`
}

type Outbox struct {
	MaxAttempts    int      `env:"MAX_ATTEMPTS"`
	InitialBackoff Duration `env:"INITIAL_BACKOFF"`
	MaxBackoff     Duration `env:"MAX_BACKOFF"`
}

type Retention struct {
	JobHistory           Duration `env:"JOB_HISTORY"`
	UnmanagedItemsUnseen Duration `env:"UNMANAGED_ITEMS_UNSEEN"`
	ProcessedOutbox      Duration `env:"PROCESSED_OUTBOX"`
	DeletedItems         Duration `env:"DELETED_ITEMS"`
}

type Delivery struct {
	SignedLinkTTL        Duration `env:"SIGNED_LINK_TTL"`
	MaxStreamsPerUser    int      `env:"MAX_STREAMS_PER_USER"`
	MaxConcurrentStreams int      `env:"MAX_CONCURRENT_STREAMS"`
	// SigningKeys come from DEBROXY_SIGNING_KEYS and DEBROXY_SIGNING_KEY_<ID>.
	SigningKeys []SigningKey
}

// SigningKey is one Signed Link key; the first key signs, all keys verify (spec §7.1).
type SigningKey struct {
	ID     string
	Secret Secret
}

// Provider is one Provider Account. Its variables are
// DEBROXY_PROVIDER_<ID>_<KEY> (spec §11).
type Provider struct {
	ID          string
	Kind        string `env:"KIND"`
	DisplayName string `env:"DISPLAY_NAME"`
	// Enabled defaults to true when unset.
	Enabled   *bool    `env:"ENABLED"`
	APIKey    Secret   `env:"API_KEY"`
	UserAgent string   `env:"USER_AGENT"`
	BaseURL   string   `env:"BASE_URL"`
	Timeout   Duration `env:"TIMEOUT"`
	Retry     Retry    `env:"RETRY"`
	// CircuitBreaker protects the account from hammering a failing provider (NFR-09).
	CircuitBreaker  CircuitBreaker `env:"CIRCUIT_BREAKER"`
	Egress          Egress         `env:"EGRESS"`
	CDNLinkCacheMax Duration       `env:"CDN_LINK_CACHE_MAX"`
	// RateBudgets come from DEBROXY_PROVIDER_<ID>_RATE_BUDGET_<CLASS>.
	RateBudgets map[string]RateBudget
	// AbsenceGrace overrides job.absenceGrace for this provider (spec §5.6).
	AbsenceGrace *Duration `env:"ABSENCE_GRACE"`
	// CDNProbeHosts overrides the adapter's CDN probe hosts of the startup
	// egress check: one concrete host (or host:port) per CDN domain (spec §9.3).
	CDNProbeHosts        []string `env:"CDN_PROBE_HOSTS"`
	DisabledCapabilities []string `env:"DISABLED_CAPABILITIES"`
	Webhook              Webhook  `env:"WEBHOOK"`
}

// IsEnabled reports the effective enabled flag.
func (p Provider) IsEnabled() bool { return p.Enabled == nil || *p.Enabled }

type Retry struct {
	MaxAttempts int      `env:"MAX_ATTEMPTS"`
	BaseDelay   Duration `env:"BASE_DELAY"`
}

type CircuitBreaker struct {
	FailureThreshold int      `env:"FAILURE_THRESHOLD"`
	OpenDuration     Duration `env:"OPEN_DURATION"`
}

type Egress struct {
	// IPFamily is ipv4 or ipv6 (DEC-26).
	IPFamily     string `env:"IP_FAMILY"`
	LocalAddress string `env:"LOCAL_ADDRESS"`
}

type RateBudget struct {
	Requests int
	Per      Duration
}

type Webhook struct {
	Enabled bool   `env:"ENABLED"`
	Secret  Secret `env:"SECRET"`
}
