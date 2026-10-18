package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/feruzabad/debroxy/internal/common/server/httperr"
)

// Recover turns a panic in a handler into a logged 500 instead of a dropped
// connection. http.ErrAbortHandler is re-panicked: it is the standard way to
// abort a streaming response.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}
				httperr.RespondWithSlugError(w, r, logger, fmt.Errorf("panic: %v", recovered))
			}()
			next.ServeHTTP(w, r)
		})
	}
}
