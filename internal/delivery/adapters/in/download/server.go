// Package download is the streaming adapter for GET and HEAD
// /dl/{token}/{name} (spec §7.3, ADR 0010 rules 14–22). The token is the
// credential; the access log never shows it (NFR-03).
package download

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
)

// bufferSize bounds the copy buffer per stream (ADR 0010 rule 21).
const bufferSize = 32 << 10

// RegisterRoutes adds the download routes to mux.
func RegisterRoutes(mux *http.ServeMux, open in.OpenDownloadHandler, logger *slog.Logger) {
	if open == nil {
		panic("nil openDownload")
	}
	s := server{open: open, logger: logger}
	HandlerWithOptions(s, StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httperr.RespondWithSlugError(w, r, logger, commonerrors.NewIncorrectInputError(err.Error(), "invalid-request"))
		},
	})
}

type server struct {
	open   in.OpenDownloadHandler
	logger *slog.Logger
}

var _ ServerInterface = server{}

func (s server) Download(w http.ResponseWriter, r *http.Request, token string, name string) {
	s.serve(w, r, token, name, http.MethodGet)
}

func (s server) DownloadHead(w http.ResponseWriter, r *http.Request, token string, name string) {
	s.serve(w, r, token, name, http.MethodHead)
}

// serve opens the stream with the request context, so a disconnect cancels
// the upstream request, copies the status and the whitelisted headers, and
// streams the body. After the first byte an upstream error just ends the
// response.
func (s server) serve(w http.ResponseWriter, r *http.Request, token, name, method string) {
	download, err := s.open.Handle(r.Context(), in.OpenDownload{
		Token: token, Name: name, Method: method, Range: r.Header.Get("Range"), IfRange: r.Header.Get("If-Range"),
	})
	if err != nil {
		if errors.Is(err, r.Context().Err()) && r.Context().Err() != nil {
			return // the client is gone
		}
		httperr.RespondWithSlugError(w, r, s.logger, err)
		return
	}
	defer func() {
		if err := download.Body.Close(); err != nil {
			s.logger.DebugContext(r.Context(), "close download", "error", err.Error())
		}
	}()
	header := w.Header()
	for key, value := range download.Headers {
		header.Set(key, value)
	}
	if download.RangeSupported {
		header.Set("Accept-Ranges", "bytes")
	} else {
		header.Set("Accept-Ranges", "none")
	}
	header.Set("Content-Disposition", contentDisposition(download.FileName))
	w.WriteHeader(download.Status)
	if method == http.MethodHead {
		return
	}
	// Send the headers now: the first upstream bytes may take a while.
	_ = http.NewResponseController(w).Flush()
	if _, err := io.CopyBuffer(w, download.Body, make([]byte, bufferSize)); err != nil {
		s.logger.DebugContext(r.Context(), "download ended early", "error", err.Error())
	}
}

// contentDisposition always sets attachment with an RFC 5987 file name;
// the name is already sanitized (ADR 0010 rules 13, 18).
func contentDisposition(name string) string {
	var b strings.Builder
	b.WriteString("attachment; filename*=UTF-8''")
	for _, c := range []byte(name) {
		if isAttrChar(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isAttrChar is RFC 5987's attr-char.
func isAttrChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
