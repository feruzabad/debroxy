// Package webhook is the provider webhook adapter (spec §3.2, ddd-rules
// IN-5): it passes the raw request to HandleProviderWebhook, which checks
// the path secret through provider before anything parses the body.
package webhook

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// maxBody bounds a webhook body; provider webhooks are small JSON objects.
const maxBody = 64 << 10

var errTooLarge = commonerrors.NewIncorrectInputError("the webhook body is too large", "webhook-too-large")

// RegisterRoutes adds the webhook route to mux. The auth middleware lets
// it through unauthenticated; the access log redacts its path.
func RegisterRoutes(mux *http.ServeMux, handle in.HandleProviderWebhookHandler, logger *slog.Logger) {
	if handle == nil {
		panic("nil handleProviderWebhook")
	}
	respondWithError := func(w http.ResponseWriter, r *http.Request, err error) {
		httperr.RespondWithSlugError(w, r, logger, err)
	}
	HandlerWithOptions(server{handle: handle, respondWithError: respondWithError}, StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			respondWithError(w, r, commonerrors.NewIncorrectInputError(err.Error(), "invalid-request"))
		},
	})
}

type server struct {
	handle           in.HandleProviderWebhookHandler
	respondWithError func(http.ResponseWriter, *http.Request, error)
}

var _ ServerInterface = server{}

func (s server) HandleProviderWebhook(w http.ResponseWriter, r *http.Request, providerID string, secret string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	var maxBytes *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytes):
		s.respondWithError(w, r, errTooLarge)
		return
	case err != nil:
		s.respondWithError(w, r, commonerrors.NewIncorrectInputError("unreadable webhook body", "invalid-request"))
		return
	}
	err = s.handle.Handle(r.Context(), in.HandleProviderWebhook{
		ProviderID: providerID, PathSecret: secret, Headers: r.Header.Clone(), Body: body,
	})
	if err != nil {
		s.respondWithError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
