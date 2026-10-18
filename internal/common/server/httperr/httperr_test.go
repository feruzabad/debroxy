package httperr_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
)

func TestRespondWithSlugError_maps_every_error_type(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err        commonerrors.Error
		wantStatus int
	}{
		{commonerrors.NewIncorrectInputError("m", "invalid-source"), http.StatusBadRequest},
		{commonerrors.NewAuthenticationError("m", "invalid-credentials"), http.StatusUnauthorized},
		{commonerrors.NewAuthorizationError("m", "forbidden"), http.StatusForbidden},
		{commonerrors.NewNotFoundError("m", "job-not-found"), http.StatusNotFound},
		{commonerrors.NewConflictError("m", "job-already-exists"), http.StatusConflict},
		{commonerrors.NewQuotaExceededError("m", "slot-quota-exceeded"), http.StatusTooManyRequests},
		{commonerrors.NewUnsupportedError("m", "capability-not-supported"), http.StatusNotImplemented},
		{commonerrors.NewProviderUnavailableError("m", "provider-unavailable"), http.StatusBadGateway},
		{commonerrors.NewServiceUnavailableError("m", "too-many-streams"), http.StatusServiceUnavailable},
	}

	for _, tc := range testCases {
		t.Run(tc.err.Slug(), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()

			// Wrapping must not hide the slug (ddd-rules A-6).
			httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
				slog.New(slog.DiscardHandler), fmt.Errorf("context: %w", tc.err))

			assert.Equal(t, tc.wantStatus, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			var body httperr.Body
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, httperr.Body{Slug: tc.err.Slug(), Message: "m"}, body)
		})
	}
}

func TestRespondWithSlugError_hides_details_of_unknown_errors(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	response := httptest.NewRecorder()

	httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
		slog.New(slog.NewJSONHandler(&logs, nil)), errors.New("database file /var/lib/x is corrupt"))

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.JSONEq(t, `{"slug":"internal-error","message":"internal error"}`, response.Body.String())
	assert.Contains(t, logs.String(), "is corrupt", "details go to the log only")
}

// hintedError is a slug error with a retry hint used only in these tests.
type hintedError struct {
	slugError commonerrors.Error
	hint      time.Duration
}

func (e hintedError) Error() string                     { return e.slugError.Error() }
func (e hintedError) Slug() string                      { return e.slugError.Slug() }
func (e hintedError) ErrorType() commonerrors.ErrorType { return e.slugError.ErrorType() }
func (e hintedError) RetryAfter() time.Duration         { return e.hint }

func TestRespondWithSlugError_retry_hint(t *testing.T) {
	t.Parallel()

	t.Run("hinted", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		err := hintedError{slugError: commonerrors.NewQuotaExceededError("rate exceeded", "test-rate"), hint: 1500 * time.Millisecond}

		httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
			slog.New(slog.DiscardHandler), err)

		assert.Equal(t, http.StatusTooManyRequests, response.Code)
		assert.Equal(t, "2", response.Header().Get("Retry-After"))
		assert.JSONEq(t, `{"slug":"test-rate","message":"rate exceeded","retryAfter":2}`, response.Body.String())
	})

	t.Run("sub_second", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		err := hintedError{slugError: commonerrors.NewQuotaExceededError("rate exceeded", "test-rate"), hint: 100 * time.Millisecond}

		httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
			slog.New(slog.DiscardHandler), err)

		assert.Equal(t, "1", response.Header().Get("Retry-After"))
		assert.JSONEq(t, `{"slug":"test-rate","message":"rate exceeded","retryAfter":1}`, response.Body.String())
	})

	t.Run("provider_unavailable", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		err := hintedError{slugError: commonerrors.NewProviderUnavailableError("provider torbox is unavailable; try again later", "provider-unavailable"), hint: 30 * time.Second}

		httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
			slog.New(slog.DiscardHandler), err)

		assert.Equal(t, http.StatusBadGateway, response.Code)
		assert.Equal(t, "30", response.Header().Get("Retry-After"))
		assert.JSONEq(t, `{"slug":"provider-unavailable","message":"provider torbox is unavailable; try again later","retryAfter":30}`, response.Body.String())
	})

	t.Run("unhinted", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		err := commonerrors.NewQuotaExceededError("rate exceeded", "test-rate")

		httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
			slog.New(slog.DiscardHandler), err)

		assert.Equal(t, http.StatusTooManyRequests, response.Code)
		assert.Empty(t, response.Header().Get("Retry-After"))
		assert.JSONEq(t, `{"slug":"test-rate","message":"rate exceeded"}`, response.Body.String())
	})

	t.Run("non_slug", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()

		httperr.RespondWithSlugError(response, httptest.NewRequest(http.MethodGet, "/", nil),
			slog.New(slog.DiscardHandler), errors.New("boom"))

		assert.Equal(t, http.StatusInternalServerError, response.Code)
		assert.Empty(t, response.Header().Get("Retry-After"))
		assert.JSONEq(t, `{"slug":"internal-error","message":"internal error"}`, response.Body.String())
	})
}
