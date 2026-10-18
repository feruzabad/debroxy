package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// minimumSecretBytes applies to webhook secrets (spec §11: >= 32 random
// bytes, RISK-07) and Signed Link signing keys.
const minimumSecretBytes = 32

// rateBudgetNames are the budget classes each provider kind has (spec §9.4).
var rateBudgetNames = map[string][]string{
	KindTorBox:     {"general", "uncachedCreate", "cdnZip"},
	KindRealDebrid: {"general"},
}

// ValidationError lists every problem found in a configuration.
type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return "invalid configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

type validator struct {
	problems []string
}

func (v *validator) addf(field, format string, args ...any) {
	v.problems = append(v.problems, field+": "+fmt.Sprintf(format, args...))
}

func (v *validator) positiveDuration(field string, d Duration) {
	if d <= 0 {
		v.addf(field, "must be a positive duration, got %q", d.Std().String())
	}
}

func (v *validator) positive(field string, n int) {
	if n <= 0 {
		v.addf(field, "must be positive, got %d", n)
	}
}

// secret checks a secret variable; name_FILE is the alternative (spec §11).
func (v *validator) secret(name string, secret Secret, minimumBytes int) {
	switch {
	case secret.IsZero():
		v.addf(name, "must be set (or %s%s)", name, fileSuffix)
	case len(secret.Reveal()) < minimumBytes:
		v.addf(name, "must hold at least %d bytes", minimumBytes)
	}
}

func validate(v *validator, cfg *Config) {
	validateServer(v, &cfg.Server)
	validateLog(v, cfg.Log)
	validateStorage(v, cfg.DataDir, cfg.Database)
	validateIdentity(v, cfg.Identity)
	validateBootstrap(v, cfg.Bootstrap)
	validateJob(v, cfg.Job)
	validateDelivery(v, cfg.Delivery)
	validateProviders(v, cfg.Providers)
}

func validateServer(v *validator, s *Server) {
	if s.Listen == "" {
		v.addf("DEBROXY_SERVER_LISTEN", "must not be empty")
	}
	publicURL, err := url.Parse(s.PublicBaseURL)
	switch {
	case s.PublicBaseURL == "":
		v.addf("DEBROXY_SERVER_PUBLIC_BASE_URL", "must be set")
	case err != nil || (publicURL.Scheme != "https" && publicURL.Scheme != "http") || publicURL.Host == "":
		v.addf("DEBROXY_SERVER_PUBLIC_BASE_URL", "must be an absolute http(s) URL, got %q", s.PublicBaseURL)
	case strings.Trim(publicURL.Path, "/") != "" || publicURL.RawQuery != "" || publicURL.Fragment != "":
		v.addf("DEBROXY_SERVER_PUBLIC_BASE_URL", "must not have a path, query or fragment, got %q", s.PublicBaseURL)
	default:
		s.PublicURL = &url.URL{Scheme: publicURL.Scheme, Host: publicURL.Host}
	}
	s.TrustedProxyRange = nil
	for i, entry := range s.TrustedProxies {
		prefix, err := parsePrefix(entry)
		if err != nil {
			v.addf(fmt.Sprintf("DEBROXY_SERVER_TRUSTED_PROXIES[%d]", i), "must be a CIDR or an IP address, got %q", entry)
			continue
		}
		s.TrustedProxyRange = append(s.TrustedProxyRange, prefix)
	}
	v.positiveDuration("DEBROXY_SERVER_SHUTDOWN_TIMEOUT", s.ShutdownTimeout)
}

func parsePrefix(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		return prefix.Masked(), err
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func validateLog(v *validator, l Log) {
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, l.Level) {
		v.addf("DEBROXY_LOG_LEVEL", "must be one of debug, info, warn, error, got %q", l.Level)
	}
}

func validateStorage(v *validator, dataDir string, d Database) {
	if dataDir == "" {
		v.addf(Prefix+"_DATA_DIR", "must be set")
	} else if info, err := os.Stat(dataDir); err != nil || !info.IsDir() {
		v.addf(Prefix+"_DATA_DIR", "must be an existing directory, got %q", dataDir)
	}
	v.positive(Prefix+"_DATABASE_READER_CONNECTIONS", d.ReaderConnections)
}

func validateBootstrap(v *validator, b Bootstrap) {
	const username, password = Prefix + "_BOOTSTRAP_ADMIN_USERNAME", Prefix + "_BOOTSTRAP_ADMIN_PASSWORD"
	switch {
	case b.Username != "" && b.Password.IsZero():
		v.addf(password, "must be set (or %s%s) when %s is set", password, fileSuffix, username)
	case b.Username == "" && !b.Password.IsZero():
		v.addf(username, "must be set when %s is set", password)
	}
}

func validateIdentity(v *validator, i Identity) {
	v.positiveDuration("DEBROXY_IDENTITY_SESSION_ABSOLUTE_LIFETIME", i.Session.AbsoluteLifetime)
	v.positiveDuration("DEBROXY_IDENTITY_SESSION_IDLE_TIMEOUT", i.Session.IdleTimeout)
	if i.Session.IdleTimeout > i.Session.AbsoluteLifetime {
		v.addf("DEBROXY_IDENTITY_SESSION_IDLE_TIMEOUT", "must not exceed DEBROXY_IDENTITY_SESSION_ABSOLUTE_LIFETIME")
	}
	if !i.OIDC.Enabled {
		return
	}
	issuer, err := url.Parse(i.OIDC.IssuerURL)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" {
		v.addf("DEBROXY_IDENTITY_OIDC_ISSUER_URL", "must be an absolute https URL, got %q", i.OIDC.IssuerURL)
	}
	if i.OIDC.ClientID == "" {
		v.addf("DEBROXY_IDENTITY_OIDC_CLIENT_ID", "must be set when OIDC is enabled")
	}
	v.secret(Prefix+"_IDENTITY_OIDC_CLIENT_SECRET", i.OIDC.ClientSecret, 1)
	if !slices.Contains(i.OIDC.Scopes, "openid") {
		v.addf("DEBROXY_IDENTITY_OIDC_SCOPES", "must contain openid")
	}
}

func validateJob(v *validator, j Job) {
	v.positive("DEBROXY_JOB_DEFAULT_SLOT_QUOTA", j.DefaultSlotQuota)
	if j.UploadLimitBytes <= 0 {
		v.addf("DEBROXY_JOB_UPLOAD_LIMIT_BYTES", "must be positive, got %d", j.UploadLimitBytes)
	}
	v.positive("DEBROXY_JOB_SUBMIT_RATE_PER_HOUR", j.SubmitRate.PerHour)
	v.positive("DEBROXY_JOB_SUBMIT_RATE_CACHED_PER_HOUR", j.SubmitRate.CachedPerHour)
	v.positive("DEBROXY_JOB_FORCED_REFRESH_PER_USER_PER_MINUTE", j.ForcedRefresh.PerUserPerMinute)
	v.positive("DEBROXY_JOB_EVENTS_MAX_STREAMS_PER_USER", j.Events.MaxStreamsPerUser)
	v.positive("DEBROXY_JOB_EVENTS_MAX_STREAMS_TOTAL", j.Events.MaxStreamsTotal)
	for _, check := range []struct {
		field string
		value Duration
	}{
		{"DEBROXY_JOB_REFRESH_MAX_AGE", j.RefreshMaxAge},
		{"DEBROXY_JOB_SAFETY_REFRESH_ACTIVE", j.SafetyRefresh.Active},
		{"DEBROXY_JOB_SAFETY_REFRESH_IDLE", j.SafetyRefresh.Idle},
		{"DEBROXY_JOB_STALL_THRESHOLD", j.StallThreshold},
		{"DEBROXY_JOB_SUBMISSION_TIMEOUT", j.SubmissionTimeout},
		{"DEBROXY_JOB_SUBMIT_REQUEST_DEADLINE", j.SubmitRequestDeadline},
		{"DEBROXY_JOB_ABSENCE_GRACE", j.AbsenceGrace},
		{"DEBROXY_JOB_REFRESH_BACKOFF_INITIAL", j.RefreshBackoff.Initial},
		{"DEBROXY_JOB_REFRESH_BACKOFF_MAX", j.RefreshBackoff.Max},
		{"DEBROXY_JOB_WEBHOOK_REFRESH_INTERVAL", j.WebhookRefreshInterval},
		{"DEBROXY_JOB_OUTBOX_INITIAL_BACKOFF", j.Outbox.InitialBackoff},
		{"DEBROXY_JOB_OUTBOX_MAX_BACKOFF", j.Outbox.MaxBackoff},
		{"DEBROXY_JOB_RETENTION_JOB_HISTORY", j.Retention.JobHistory},
		{"DEBROXY_JOB_RETENTION_UNMANAGED_ITEMS_UNSEEN", j.Retention.UnmanagedItemsUnseen},
		{"DEBROXY_JOB_RETENTION_PROCESSED_OUTBOX", j.Retention.ProcessedOutbox},
		{"DEBROXY_JOB_RETENTION_DELETED_ITEMS", j.Retention.DeletedItems},
	} {
		v.positiveDuration(check.field, check.value)
	}
	if j.RefreshBackoff.Initial > j.RefreshBackoff.Max {
		v.addf("DEBROXY_JOB_REFRESH_BACKOFF_INITIAL", "must not exceed DEBROXY_JOB_REFRESH_BACKOFF_MAX")
	}
	if j.Outbox.InitialBackoff > j.Outbox.MaxBackoff {
		v.addf("DEBROXY_JOB_OUTBOX_INITIAL_BACKOFF", "must not exceed DEBROXY_JOB_OUTBOX_MAX_BACKOFF")
	}
	v.positive("DEBROXY_JOB_OUTBOX_MAX_ATTEMPTS", j.Outbox.MaxAttempts)
	if len(j.FollowUpRefreshes) == 0 {
		v.addf("DEBROXY_JOB_FOLLOW_UP_REFRESHES", "must not be empty")
	}
	for i, d := range j.FollowUpRefreshes {
		field := fmt.Sprintf("DEBROXY_JOB_FOLLOW_UP_REFRESHES[%d]", i)
		v.positiveDuration(field, d)
		if i > 0 && d <= j.FollowUpRefreshes[i-1] {
			v.addf(field, "must be later than the previous follow-up")
		}
	}
	// The submission timeout must exceed the follow-up schedule (spec §5.6).
	if n := len(j.FollowUpRefreshes); n > 0 && j.SubmissionTimeout <= j.FollowUpRefreshes[n-1] {
		v.addf("DEBROXY_JOB_SUBMISSION_TIMEOUT", "must exceed the last follow-up refresh (%s)", j.FollowUpRefreshes[n-1].Std())
	}
	if j.SubmitRequestDeadline >= j.SubmissionTimeout {
		v.addf("DEBROXY_JOB_SUBMIT_REQUEST_DEADLINE", "must be shorter than DEBROXY_JOB_SUBMISSION_TIMEOUT")
	}
}

func validateDelivery(v *validator, d Delivery) {
	v.positiveDuration("DEBROXY_DELIVERY_SIGNED_LINK_TTL", d.SignedLinkTTL)
	v.positive("DEBROXY_DELIVERY_MAX_STREAMS_PER_USER", d.MaxStreamsPerUser)
	v.positive("DEBROXY_DELIVERY_MAX_CONCURRENT_STREAMS", d.MaxConcurrentStreams)
	if d.MaxStreamsPerUser > d.MaxConcurrentStreams {
		v.addf("DEBROXY_DELIVERY_MAX_STREAMS_PER_USER", "must not exceed DEBROXY_DELIVERY_MAX_CONCURRENT_STREAMS")
	}
	if len(d.SigningKeys) == 0 {
		v.addf(signingKeysVariable, "needs at least one key ID")
	}
	seen := map[string]bool{}
	for _, key := range d.SigningKeys {
		// IDs that differ only in case or "-"/"_" would share one variable.
		if seen[IDSegment(key.ID)] {
			v.addf(signingKeysVariable, "duplicate key ID %q", key.ID)
		}
		seen[IDSegment(key.ID)] = true
		v.secret(SigningKeyVariable(key.ID), key.Secret, minimumSecretBytes)
	}
}

func validateProviders(v *validator, providers []Provider) {
	if len(providers) == 0 {
		v.addf(providersVariable, "needs at least one provider ID")
	}
	seen := map[string]bool{}
	for _, p := range providers {
		field := ProviderPrefix(p.ID)
		switch {
		case seen[p.ID]:
			v.addf(providersVariable, "duplicate provider ID %q", p.ID)
		case !isSlug(p.ID):
			v.addf(providersVariable, "provider IDs must be lowercase letters, digits and dashes, got %q", p.ID)
		}
		seen[p.ID] = true
		budgetNames, known := rateBudgetNames[p.Kind]
		if !known {
			v.addf(field+"_KIND", "unknown provider kind %q; known: %s, %s", p.Kind, KindTorBox, KindRealDebrid)
		}
		v.secret(field+"_API_KEY", p.APIKey, 1)
		if p.UserAgent == "" || strings.ContainsAny(p.UserAgent, "\r\n") {
			v.addf(field+"_USER_AGENT", "must be a non-empty single line")
		}
		if base, err := url.Parse(p.BaseURL); err != nil || base.Scheme != "https" || base.Host == "" {
			v.addf(field+"_BASE_URL", "must be an absolute https URL, got %q", p.BaseURL)
		}
		v.positiveDuration(field+"_TIMEOUT", p.Timeout)
		v.positive(field+"_RETRY_MAX_ATTEMPTS", p.Retry.MaxAttempts)
		v.positiveDuration(field+"_RETRY_BASE_DELAY", p.Retry.BaseDelay)
		v.positive(field+"_CIRCUIT_BREAKER_FAILURE_THRESHOLD", p.CircuitBreaker.FailureThreshold)
		v.positiveDuration(field+"_CIRCUIT_BREAKER_OPEN_DURATION", p.CircuitBreaker.OpenDuration)
		v.positiveDuration(field+"_CDN_LINK_CACHE_MAX", p.CDNLinkCacheMax)
		if p.AbsenceGrace != nil {
			v.positiveDuration(field+"_ABSENCE_GRACE", *p.AbsenceGrace)
		}
		validateEgress(v, field+"_EGRESS", p.Egress)
		if known {
			validateRateBudgets(v, field+rateBudgetSegment, p.RateBudgets, budgetNames)
		}
		for j, host := range p.CDNProbeHosts {
			if !isProbeHost(host) {
				v.addf(fmt.Sprintf("%s_CDN_PROBE_HOSTS[%d]", field, j), "must be a host name or host:port, got %q", host)
			}
		}
		if p.Webhook.Enabled {
			if p.Kind == KindRealDebrid {
				v.addf(field+"_WEBHOOK_ENABLED", "Real-Debrid offers no webhooks (spec §6.3)")
			}
			v.secret(field+"_WEBHOOK_SECRET", p.Webhook.Secret, minimumSecretBytes)
		}
	}
}

func validateEgress(v *validator, field string, e Egress) {
	if e.IPFamily != "ipv4" && e.IPFamily != "ipv6" {
		v.addf(field+"_IP_FAMILY", "must be ipv4 or ipv6, got %q", e.IPFamily)
		return
	}
	if e.LocalAddress == "" {
		return
	}
	addr, err := netip.ParseAddr(e.LocalAddress)
	switch {
	case err != nil:
		v.addf(field+"_LOCAL_ADDRESS", "must be an IP address, got %q", e.LocalAddress)
	case e.IPFamily == "ipv4" && !addr.Unmap().Is4(), e.IPFamily == "ipv6" && !addr.Is6():
		v.addf(field+"_LOCAL_ADDRESS", "%s is not an %s address", e.LocalAddress, e.IPFamily)
	}
}

func validateRateBudgets(v *validator, field string, budgets map[string]RateBudget, allowed []string) {
	names := make([]string, 0, len(budgets))
	for name := range budgets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		budget := budgets[name]
		variable := field + name // an unknown class keeps its variable segment
		if slices.Contains(allowed, name) {
			variable = field + RateBudgetSegment(name)
		}
		if !slices.Contains(allowed, name) {
			known := make([]string, len(allowed))
			for i, class := range allowed {
				known[i] = RateBudgetSegment(class)
			}
			v.addf(variable, "unknown budget for this provider kind; known: %s", strings.Join(known, ", "))
			continue
		}
		v.positive(variable+" requests", budget.Requests)
		v.positiveDuration(variable+" duration", budget.Per)
	}
}

// isProbeHost accepts "host" or "host:port" for the startup egress check.
func isProbeHost(s string) bool {
	host := s
	if h, port, err := net.SplitHostPort(s); err == nil {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return false
		}
		host = h
	}
	return host != "" && !strings.ContainsAny(host, "/* :")
}

func isSlug(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return s != ""
}
