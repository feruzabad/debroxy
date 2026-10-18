package httpclient

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

var start = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// advancingSleeper moves the fixed clock instead of sleeping.
func advancingSleeper(c *clock.Fixed) Sleeper {
	return func(_ context.Context, d time.Duration) error {
		c.Advance(d)
		return nil
	}
}

func newTestBudgets(t *testing.T, c *clock.Fixed, classes map[string]BucketConfig) *Budgets {
	t.Helper()
	budgets, err := newBudgets(classes, c, advancingSleeper(c), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return budgets
}

func rateHeaders(limit, remaining int, reset time.Time) http.Header {
	header := http.Header{}
	header.Set("X-Ratelimit-Limit", strconv.Itoa(limit))
	header.Set("X-Ratelimit-Remaining", strconv.Itoa(remaining))
	header.Set("X-Ratelimit-Reset", strconv.FormatFloat(float64(reset.UnixNano())/1e9, 'f', 6, 64))
	return header
}

func TestBudgets_static_window_waits_or_reports_retry_after(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{"api": {Requests: 4, Per: time.Minute}})
	key := BucketKey{Class: "api"}
	ctx := context.Background()

	// Header-less buckets start with half their budget (ADR 0013 rule 9).
	for range 2 {
		reservation, err := budgets.Reserve(ctx, key, 0)
		require.NoError(t, err)
		reservation.Complete(http.StatusOK, nil)
	}
	_, err := budgets.Reserve(ctx, key, 30*time.Second)
	assert.Equal(t, BudgetExhaustedError{Bucket: key, RetryAfter: time.Minute}, err, "too far to wait: nothing is sent")

	reservation, err := budgets.Reserve(ctx, key, time.Minute)
	require.NoError(t, err, "waits for the reset when allowed")
	reservation.Complete(http.StatusOK, nil)
	assert.Equal(t, start.Add(time.Minute), c.Now())

	for range 3 {
		reservation, err := budgets.Reserve(ctx, key, 0)
		require.NoError(t, err, "after the reset the full budget applies")
		reservation.Complete(http.StatusOK, nil)
	}
	_, err = budgets.Reserve(ctx, key, 0)
	assert.Error(t, err)
}

func TestBudgets_headers_drive_the_bucket(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{"general": {Requests: 300, Per: time.Minute, ReportsHeaders: true}})
	key := BucketKey{Class: "general", Endpoint: "/list"}
	ctx := context.Background()

	reservation, err := budgets.Reserve(ctx, key, 0)
	require.NoError(t, err)
	reservation.Complete(http.StatusOK, rateHeaders(300, 0, start.Add(20*time.Second)))

	_, err = budgets.Reserve(ctx, key, 0)
	assert.Equal(t, BudgetExhaustedError{Bucket: key, RetryAfter: 20 * time.Second}, err, "remaining 0 from the provider")
}

func TestBudgets_reported_limit_selects_the_class(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{
		"general":        {Requests: 300, Per: time.Minute, ReportsHeaders: true},
		"uncachedCreate": {Requests: 60, Per: time.Hour, ReportsHeaders: true},
	})
	ctx := context.Background()
	general := BucketKey{Class: "general", Endpoint: "/create"}
	hourly := BucketKey{Class: "uncachedCreate", Endpoint: "/create"}

	// Charged to general, but the provider answers with the hourly bucket's limit.
	reservation, err := budgets.Reserve(ctx, general, 0)
	require.NoError(t, err)
	reservation.Complete(http.StatusOK, rateHeaders(60, 0, start.Add(time.Hour)), "uncachedCreate")

	_, err = budgets.Reserve(ctx, hourly, 0)
	assert.Equal(t, BudgetExhaustedError{Bucket: hourly, RetryAfter: time.Hour}, err, "the hourly bucket was corrected")
	reservation, err = budgets.Reserve(ctx, general, 0)
	assert.NoError(t, err, "general keeps its own count")
	reservation.Cancel()
}

func TestBudgets_invalid_headers_are_ignored(t *testing.T) {
	t.Parallel()

	testCases := map[string]http.Header{
		"past_reset":         rateHeaders(300, 0, start.Add(-time.Second)),
		"negative_remaining": rateHeaders(300, -1, start.Add(time.Minute)),
		"unparseable":        {"X-Ratelimit-Limit": {"lots"}, "X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"soon"}},
	}

	for name, header := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := clock.NewFixed(start)
			budgets := newTestBudgets(t, c, map[string]BucketConfig{"api": {Requests: 300, Per: time.Minute, ReportsHeaders: true}})
			key := BucketKey{Class: "api"}

			reservation, err := budgets.Reserve(context.Background(), key, 0)
			require.NoError(t, err)
			reservation.Complete(http.StatusOK, header)

			reservation, err = budgets.Reserve(context.Background(), key, 0)
			require.NoError(t, err, "own count kept: 298 left")
			reservation.Cancel()
		})
	}
}

func TestBudgets_retry_after_on_429_empties_the_bucket(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{"api": {Requests: 300, Per: time.Minute}})
	key := BucketKey{Class: "api"}

	reservation, err := budgets.Reserve(context.Background(), key, 0)
	require.NoError(t, err)
	reservation.Complete(http.StatusTooManyRequests, http.Header{"Retry-After": {"42"}})

	_, err = budgets.Reserve(context.Background(), key, 0)
	assert.Equal(t, BudgetExhaustedError{Bucket: key, RetryAfter: 42 * time.Second}, err)
}

func TestBudgets_header_reporting_bucket_is_single_flight_until_first_headers(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{"api": {Requests: 300, Per: time.Minute, ReportsHeaders: true}})
	key := BucketKey{Class: "api"}
	ctx := context.Background()

	first, err := budgets.Reserve(ctx, key, 0)
	require.NoError(t, err)

	secondGranted := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		second, err := budgets.Reserve(ctx, key, 0)
		if assert.NoError(t, err) {
			close(secondGranted)
			second.Complete(http.StatusOK, nil)
		}
	})
	select {
	case <-secondGranted:
		t.Fatal("a second call ran before the first one's headers arrived")
	case <-time.After(50 * time.Millisecond):
	}

	first.Complete(http.StatusOK, rateHeaders(300, 250, start.Add(time.Minute)))
	wg.Wait()

	// Headers seen: calls now run in parallel.
	a, err := budgets.Reserve(ctx, key, 0)
	require.NoError(t, err)
	b, err := budgets.Reserve(ctx, key, 0)
	require.NoError(t, err)
	a.Cancel()
	b.Cancel()
}

func TestBudgets_cancel_returns_the_token(t *testing.T) {
	t.Parallel()
	c := clock.NewFixed(start)
	budgets := newTestBudgets(t, c, map[string]BucketConfig{"api": {Requests: 2, Per: time.Minute}})
	key := BucketKey{Class: "api"}

	reservation, err := budgets.Reserve(context.Background(), key, 0)
	require.NoError(t, err)
	reservation.Cancel()

	again, err := budgets.Reserve(context.Background(), key, 0)
	require.NoError(t, err, "the half budget (1) is available again")
	again.Cancel()
}

func TestNewBudgets_rejects_non_positive_budgets(t *testing.T) {
	t.Parallel()

	_, err := newBudgets(map[string]BucketConfig{"api": {Requests: 0, Per: time.Minute}}, clock.NewFixed(start), SleepWithContext, slog.New(slog.DiscardHandler))

	assert.Error(t, err)
}
