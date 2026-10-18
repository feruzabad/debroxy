package httpclient

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

// BucketKey names one rate bucket: a class chosen by the caller and an
// endpoint (empty when one bucket covers the whole API). ADR 0013 rule 6.
type BucketKey struct {
	Class    string
	Endpoint string
}

// BucketConfig is the static budget of a class: Requests per window.
// ReportsHeaders means the provider sends x-ratelimit-* headers for it.
type BucketConfig struct {
	Requests       int
	Per            time.Duration
	ReportsHeaders bool
}

// BudgetExhaustedError: the bucket is empty and its reset is further away
// than the caller may wait. Nothing was sent.
type BudgetExhaustedError struct {
	Bucket     BucketKey
	RetryAfter time.Duration
}

func (e BudgetExhaustedError) Error() string {
	return fmt.Sprintf("rate budget %s exhausted; retry in %s", e.Bucket.Class, e.RetryAfter.Round(time.Second))
}

// Sleeper waits for d or until ctx ends. Tests replace it to move a fixed clock.
type Sleeper func(ctx context.Context, d time.Duration) error

// SleepWithContext is the real Sleeper.
func SleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Budgets holds the rate buckets of one client. Each bucket mirrors the
// provider's fixed window: a limit, the calls remaining and the reset time.
type Budgets struct {
	mu      sync.Mutex
	classes map[string]BucketConfig
	buckets map[BucketKey]*bucket
	clock   clock.Clock
	sleep   Sleeper
	logger  *slog.Logger
	logged  map[string]bool
}

type bucket struct {
	cfg       BucketConfig
	limit     int
	remaining int
	resetAt   time.Time
	// singleFlight: a header-reporting bucket allows one call at a time until
	// its first valid headers arrive after startup (ADR 0013 rule 9).
	singleFlight bool
	inFlight     int
	changed      chan struct{}
}

func newBudgets(classes map[string]BucketConfig, clk clock.Clock, sleep Sleeper, logger *slog.Logger) (*Budgets, error) {
	for name, cfg := range classes {
		if cfg.Requests <= 0 || cfg.Per <= 0 {
			return nil, fmt.Errorf("rate budget %q must have positive requests and window", name)
		}
	}
	return &Budgets{
		classes: classes, buckets: map[BucketKey]*bucket{},
		clock: clk, sleep: sleep, logger: logger, logged: map[string]bool{},
	}, nil
}

// bucketFor creates buckets lazily. A header-less bucket gets half of its
// static budget for its first window, since the provider may still count
// calls from before a restart (ADR 0013 rule 9).
func (b *Budgets) bucketFor(key BucketKey, now time.Time) (*bucket, error) {
	if existing, ok := b.buckets[key]; ok {
		return existing, nil
	}
	cfg, ok := b.classes[key.Class]
	if !ok {
		return nil, fmt.Errorf("unknown rate budget class %q", key.Class)
	}
	created := &bucket{
		cfg: cfg, limit: cfg.Requests, remaining: cfg.Requests,
		resetAt: now.Add(cfg.Per), singleFlight: cfg.ReportsHeaders, changed: make(chan struct{}),
	}
	if !cfg.ReportsHeaders {
		created.remaining = max(cfg.Requests/2, 1)
	}
	b.buckets[key] = created
	return created, nil
}

func (k *bucket) refill(now time.Time) {
	if now.Before(k.resetAt) {
		return
	}
	k.remaining = k.limit
	k.resetAt = now.Add(k.cfg.Per)
}

func (k *bucket) notify() {
	close(k.changed)
	k.changed = make(chan struct{})
}

// Reservation is one token taken from a bucket. Exactly one of Complete or
// Cancel must be called.
type Reservation struct {
	budgets *Budgets
	key     BucketKey
}

// Reserve takes a token from key's bucket. If the bucket is empty it waits
// for the reset when that is at most maxWait away; otherwise it returns
// BudgetExhaustedError without waiting.
func (b *Budgets) Reserve(ctx context.Context, key BucketKey, maxWait time.Duration) (*Reservation, error) {
	for {
		b.mu.Lock()
		now := b.clock.Now()
		k, err := b.bucketFor(key, now)
		if err != nil {
			b.mu.Unlock()
			return nil, err
		}
		k.refill(now)
		if k.singleFlight && k.inFlight > 0 {
			changed := k.changed
			b.mu.Unlock()
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if k.remaining > 0 {
			k.remaining--
			k.inFlight++
			b.mu.Unlock()
			return &Reservation{budgets: b, key: key}, nil
		}
		wait := k.resetAt.Sub(now)
		b.mu.Unlock()
		if wait > maxWait {
			return nil, BudgetExhaustedError{Bucket: key, RetryAfter: wait}
		}
		if err := b.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// Cancel returns the token of a call that was never sent.
func (r *Reservation) Cancel() {
	b := r.budgets
	b.mu.Lock()
	defer b.mu.Unlock()
	k := b.buckets[r.key]
	k.remaining = min(k.remaining+1, k.limit)
	k.inFlight--
	k.notify()
}

// Complete records the response of a sent call and applies its rate
// headers (ADR 0013 rule 6). alternatives are other classes the same
// endpoint is known to be charged to; the reported limit selects among
// them, so a call charged to the wrong class corrects the right bucket.
func (r *Reservation) Complete(status int, header http.Header, alternatives ...string) {
	b := r.budgets
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	charged := b.buckets[r.key]
	defer func() {
		charged.inFlight--
		charged.notify()
	}()

	if status == http.StatusTooManyRequests {
		if retryAfter, ok := parseRetryAfter(header.Get("Retry-After")); ok {
			charged.remaining = 0
			charged.resetAt = now.Add(retryAfter)
		}
	}
	limit, remaining, resetAt, ok := b.parseRateHeaders(header, now)
	if !ok {
		return
	}
	target := charged
	if limit != charged.limit {
		target = nil
		for _, class := range alternatives {
			if cfg, known := b.classes[class]; known && cfg.Requests == limit {
				target, _ = b.bucketFor(BucketKey{Class: class, Endpoint: r.key.Endpoint}, now)
				break
			}
		}
		if target == nil {
			b.logOnce("unknown-limit:"+strconv.Itoa(limit), "rate limit header matches no known budget; adopting it",
				"class", r.key.Class, "limit", limit)
			target = charged
		}
	}
	target.limit = limit
	target.remaining = remaining
	target.resetAt = resetAt
	target.singleFlight = false
	if target != charged {
		target.notify()
	}
}

// parseRateHeaders reads x-ratelimit-limit, -remaining and -reset (Unix
// seconds, fractions allowed). A missing, unparseable or past reset, or a
// negative remaining count, makes the whole set unusable.
func (b *Budgets) parseRateHeaders(header http.Header, now time.Time) (limit, remaining int, resetAt time.Time, ok bool) {
	limitText, remainingText, resetText := header.Get("X-Ratelimit-Limit"), header.Get("X-Ratelimit-Remaining"), header.Get("X-Ratelimit-Reset")
	if limitText == "" && remainingText == "" && resetText == "" {
		return 0, 0, time.Time{}, false
	}
	limit, limitErr := strconv.Atoi(limitText)
	remaining, remainingErr := strconv.Atoi(remainingText)
	resetSeconds, resetErr := strconv.ParseFloat(resetText, 64)
	if limitErr != nil || remainingErr != nil || resetErr != nil || limit <= 0 || remaining < 0 ||
		math.IsNaN(resetSeconds) || math.IsInf(resetSeconds, 0) {
		b.logOnce("unparseable:"+limitText+"|"+remainingText+"|"+resetText, "ignoring unparseable rate limit headers",
			"limit", limitText, "remaining", remainingText, "reset", resetText)
		return 0, 0, time.Time{}, false
	}
	resetAt = time.Unix(0, int64(resetSeconds*float64(time.Second))).UTC()
	if !resetAt.After(now) {
		b.logOnce("past-reset", "ignoring rate limit headers with a reset in the past", "reset", resetText)
		return 0, 0, time.Time{}, false
	}
	return limit, remaining, resetAt, true
}

func parseRetryAfter(value string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func (b *Budgets) logOnce(key, msg string, args ...any) {
	if b.logged[key] {
		return
	}
	b.logged[key] = true
	b.logger.Warn(msg, args...)
}
