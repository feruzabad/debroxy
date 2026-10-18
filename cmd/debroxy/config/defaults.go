package config

import (
	"maps"
	"time"
)

// Provider kinds known to debroxy (spec §3.1).
const (
	KindTorBox     = "torbox"
	KindRealDebrid = "realdebrid"
)

// providerKindDefaults are the values of spec §9 and §11 per provider kind.
var providerKindDefaults = map[string]Provider{
	KindTorBox: {
		DisplayName: "TorBox",
		BaseURL:     "https://api.torbox.app/v1/api",
		RateBudgets: map[string]RateBudget{
			"general":        {Requests: 300, Per: Duration(time.Minute)},
			"uncachedCreate": {Requests: 60, Per: Duration(time.Hour)},
			"cdnZip":         {Requests: 60, Per: Duration(time.Minute)},
		},
	},
	KindRealDebrid: {
		DisplayName: "Real-Debrid",
		BaseURL:     "https://api.real-debrid.com/rest/1.0",
		RateBudgets: map[string]RateBudget{
			"general": {Requests: 250, Per: Duration(time.Minute)},
		},
	},
}

// Default returns the configuration with every default of the spec applied.
// Fields without a sensible default (paths, URLs, providers) stay empty and
// are caught by Validate.
func Default() Config {
	minutes := func(m int) Duration { return Duration(time.Duration(m) * time.Minute) }
	seconds := func(s int) Duration { return Duration(time.Duration(s) * time.Second) }
	hours := func(h int) Duration { return Duration(time.Duration(h) * time.Hour) }
	return Config{
		Server:   Server{Listen: ":8080", ShutdownTimeout: seconds(30)},
		Log:      Log{Level: "info"},
		Database: Database{ReaderConnections: 4},
		Identity: Identity{
			Session: Session{AbsoluteLifetime: hours(720), IdleTimeout: hours(168)},
			OIDC:    OIDC{Scopes: []string{"openid", "profile", "email"}},
		},
		Job: Job{
			DefaultSlotQuota:       3,
			UploadLimitBytes:       10 << 20,
			SubmitRate:             SubmitRate{PerHour: 10, CachedPerHour: 60},
			ForcedRefresh:          ForcedRefresh{PerUserPerMinute: 2},
			Events:                 Events{MaxStreamsPerUser: 4, MaxStreamsTotal: 256},
			RefreshMaxAge:          seconds(10),
			SafetyRefresh:          SafetyRefresh{Active: minutes(5), Idle: minutes(30)},
			StallThreshold:         minutes(30),
			SubmissionTimeout:      minutes(20),
			SubmitRequestDeadline:  seconds(10),
			AbsenceGrace:           seconds(60),
			RefreshBackoff:         Backoff{Initial: seconds(5), Max: minutes(2)},
			FollowUpRefreshes:      []Duration{seconds(30), minutes(2), minutes(5), minutes(10), minutes(15)},
			WebhookRefreshInterval: seconds(30),
			Outbox:                 Outbox{MaxAttempts: 20, InitialBackoff: seconds(1), MaxBackoff: hours(1)},
			Retention: Retention{
				JobHistory:           hours(4320),
				UnmanagedItemsUnseen: hours(168),
				ProcessedOutbox:      hours(720),
				DeletedItems:         hours(720),
			},
		},
		Delivery: Delivery{SignedLinkTTL: hours(6), MaxStreamsPerUser: 4, MaxConcurrentStreams: 64},
	}
}

// providerDefaults is a provider of the given kind with every default of
// spec §9/§11; its variables then override single fields.
func providerDefaults(id, kind string) Provider {
	kindDefaults := providerKindDefaults[kind]
	return Provider{
		ID:              id,
		DisplayName:     kindDefaults.DisplayName,
		BaseURL:         kindDefaults.BaseURL,
		UserAgent:       "debroxy/0.1",
		Timeout:         Duration(30 * time.Second),
		Retry:           Retry{MaxAttempts: 3, BaseDelay: Duration(500 * time.Millisecond)},
		CircuitBreaker:  CircuitBreaker{FailureThreshold: 5, OpenDuration: Duration(30 * time.Second)},
		Egress:          Egress{IPFamily: "ipv4"},
		CDNLinkCacheMax: Duration(30 * time.Minute),
		RateBudgets:     maps.Clone(kindDefaults.RateBudgets),
	}
}
