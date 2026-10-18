package decorator_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

type LogInWithPassword struct {
	Username string
	Password string
}

type commandHandlerFunc func(context.Context, LogInWithPassword) error

func (f commandHandlerFunc) Handle(ctx context.Context, cmd LogInWithPassword) error {
	return f(ctx, cmd)
}

type JobDetails struct{ JobID string }

type queryHandlerFunc func(context.Context, JobDetails) (string, error)

func (f queryHandlerFunc) Handle(ctx context.Context, q JobDetails) (string, error) { return f(ctx, q) }

type recordingMetricsClient struct {
	mu   sync.Mutex
	keys []string
}

func (c *recordingMetricsClient) Inc(key string, _ int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, key)
}

func TestApplyCommandDecorators(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		err        error
		wantLevel  string
		wantMetric string
	}{
		{"success_logs_at_debug", nil, "DEBUG", "commands.LogInWithPassword.success"},
		{"slug_error_logs_at_info", commonerrors.NewAuthenticationError("bad", "invalid-credentials"), "INFO", "commands.LogInWithPassword.failure"},
		{"other_error_logs_at_error", errors.New("disk full"), "ERROR", "commands.LogInWithPassword.failure"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			metrics := &recordingMetricsClient{}
			handler := decorator.ApplyCommandDecorators[LogInWithPassword](
				commandHandlerFunc(func(context.Context, LogInWithPassword) error { return tc.err }), logger, metrics)

			err := handler.Handle(context.Background(), LogInWithPassword{Username: "alice", Password: "hunter2-secret"})

			assert.Equal(t, tc.err, err, "decorators return the handler's error unchanged")
			assert.Contains(t, logs.String(), `"level":"`+tc.wantLevel+`"`)
			assert.Contains(t, logs.String(), `"command":"LogInWithPassword"`)
			assert.Contains(t, logs.String(), `"duration"`)
			assert.NotContains(t, logs.String(), "hunter2-secret", "payloads are never logged")
			assert.Equal(t, []string{tc.wantMetric}, metrics.keys)
		})
	}
}

func TestApplyQueryDecorators_returns_the_result(t *testing.T) {
	t.Parallel()
	metrics := &recordingMetricsClient{}
	handler := decorator.ApplyQueryDecorators[JobDetails, string](
		queryHandlerFunc(func(_ context.Context, q JobDetails) (string, error) { return "details of " + q.JobID, nil }),
		slog.New(slog.DiscardHandler), metrics)

	result, err := handler.Handle(context.Background(), JobDetails{JobID: "j1"})

	require.NoError(t, err)
	assert.Equal(t, "details of j1", result)
	assert.Equal(t, []string{"queries.JobDetails.success"}, metrics.keys)
}

func TestApplyCommandDecorators_panics_on_nil_dependencies(t *testing.T) {
	t.Parallel()
	handler := commandHandlerFunc(func(context.Context, LogInWithPassword) error { return nil })

	assert.PanicsWithValue(t, "nil logger", func() {
		decorator.ApplyCommandDecorators[LogInWithPassword](handler, nil, decorator.NoOpMetricsClient{})
	})
	assert.PanicsWithValue(t, "nil metricsClient", func() {
		decorator.ApplyCommandDecorators[LogInWithPassword](handler, slog.New(slog.DiscardHandler), nil)
	})
}
