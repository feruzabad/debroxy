// Package http is the Signed Link API: the generated server for
// api/openapi/delivery.yml without the download routes (ddd-rules IN-1,
// IN-2).
package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
)

var (
	errAuthenticationRequired = commonerrors.NewAuthenticationError("this operation needs a logged-in user", "authentication-required")
	errOneTarget              = commonerrors.NewIncorrectInputError("set either fileId or bundle", "invalid-request")
)

// RegisterRoutes adds the link routes to mux.
func RegisterRoutes(mux *http.ServeMux, app in.Application, logger *slog.Logger) {
	respondWithError := func(w http.ResponseWriter, r *http.Request, err error) {
		httperr.RespondWithSlugError(w, r, logger, err)
	}
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		respondWithError(w, r, commonerrors.NewIncorrectInputError(err.Error(), "invalid-request"))
	}
	strict := NewStrictHandlerWithOptions(httpServer{app: app}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		ResponseErrorHandlerFunc: respondWithError,
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: badRequest})
}

type httpServer struct {
	app in.Application
}

var _ StrictServerInterface = httpServer{}

func actorFromContext(ctx context.Context) (in.Actor, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return in.Actor{}, errAuthenticationRequired
	}
	return in.Actor{UserID: principal.UserID, Role: principal.Role}, nil
}

func (s httpServer) IssueSignedLink(ctx context.Context, request IssueSignedLinkRequestObject) (IssueSignedLinkResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	target := request.Body.Target
	bundle := target.Bundle != nil && *target.Bundle
	fileID := ""
	if target.FileId != nil {
		fileID = *target.FileId
	}
	if bundle == (fileID != "") {
		return nil, errOneTarget
	}
	link, err := s.app.Queries.IssueSignedLink.Handle(ctx, in.IssueSignedLink{Actor: actor, JobID: request.JobId, FileID: fileID, Bundle: bundle})
	if err != nil {
		return nil, err
	}
	return IssueSignedLink200JSONResponse(linkToResponse(link)), nil
}

func (s httpServer) IssueSignedLinksForAllFiles(ctx context.Context, request IssueSignedLinksForAllFilesRequestObject) (IssueSignedLinksForAllFilesResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	links, err := s.app.Queries.IssueSignedLinksForAllFiles.Handle(ctx, in.IssueSignedLinksForAllFiles{Actor: actor, JobID: request.JobId})
	if err != nil {
		return nil, err
	}
	response := SignedLinkList{Links: make([]SignedLink, 0, len(links))}
	for _, link := range links {
		response.Links = append(response.Links, linkToResponse(link))
	}
	return IssueSignedLinksForAllFiles200JSONResponse(response), nil
}

func linkToResponse(link in.SignedLinkResult) SignedLink {
	response := SignedLink{Url: link.URL, ExpiresAt: link.ExpiresAt, RangeSupported: link.RangeSupported}
	if link.Bundle {
		bundle := true
		response.Bundle = &bundle
	} else {
		fileID := link.FileID
		response.FileId = &fileID
	}
	return response
}
