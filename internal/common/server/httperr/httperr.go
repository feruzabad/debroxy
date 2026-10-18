// Package httperr maps errors to HTTP responses in one place (ddd-rules
// IN-3). Slug errors keep their slug and message; any other error becomes a
// generic 500 whose details are logged, never sent.
package httperr

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// Body is the JSON error body of every error response.
type Body struct {
	Slug       string `json:"slug"`
	Message    string `json:"message"`
	RetryAfter *int64 `json:"retryAfter,omitempty"`
}

var statusByErrorType = map[commonerrors.ErrorType]int{
	commonerrors.ErrorTypeIncorrectInput:      http.StatusBadRequest,
	commonerrors.ErrorTypeAuthentication:      http.StatusUnauthorized,
	commonerrors.ErrorTypeAuthorization:       http.StatusForbidden,
	commonerrors.ErrorTypeNotFound:            http.StatusNotFound,
	commonerrors.ErrorTypeConflict:            http.StatusConflict,
	commonerrors.ErrorTypeQuotaExceeded:       http.StatusTooManyRequests,
	commonerrors.ErrorTypeUnsupported:         http.StatusNotImplemented,
	commonerrors.ErrorTypeProviderUnavailable: http.StatusBadGateway,
	commonerrors.ErrorTypeServiceUnavailable:  http.StatusServiceUnavailable,
}

// StatusForErrorType returns the HTTP status of an ErrorType; unknown types map to 500.
func StatusForErrorType(errorType commonerrors.ErrorType) int {
	if status, ok := statusByErrorType[errorType]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// RespondWithSlugError writes the response for err.
func RespondWithSlugError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var slugErr commonerrors.SlugError
	if errors.As(err, &slugErr) {
		if status, ok := statusByErrorType[slugErr.ErrorType()]; ok {
			body := Body{Slug: slugErr.Slug(), Message: slugErr.Error()}
			var retryAfter int64
			var hinted commonerrors.RetryAfterError
			if errors.As(err, &hinted) {
				retryAfter = retryAfterSeconds(hinted.RetryAfter())
			}
			if retryAfter > 0 {
				body.RetryAfter = &retryAfter
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
			}
			respond(w, r, logger, status, body)
			return
		}
	}
	logger.ErrorContext(r.Context(), "internal error", "error", err.Error())
	respond(w, r, logger, http.StatusInternalServerError, Body{Slug: "internal-error", Message: "internal error"})
}

// retryAfterSeconds rounds a duration up to whole seconds and clamps it to at
// least 1, as required by ADR 0020.
func retryAfterSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(d.Seconds()))
}

func respond(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, body Body) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.DebugContext(r.Context(), "write error response", "error", err.Error())
	}
}
