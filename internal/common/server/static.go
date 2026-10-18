package server

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
)

// contentSecurityPolicy is ADR 0022 rule 3.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; media-src 'self'; style-src 'self'; script-src 'self'; " +
	"connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// reservedPrefixes never reach the browser client (ADR 0022 rule 2).
var reservedPrefixes = []string{"/api/", "/dl/", "/webhooks/"}

var reservedPaths = []string{"/api", "/dl", "/webhooks", "/healthz", "/readyz"}

var errNotFound = commonerrors.NewNotFoundError("no such route", "not-found")

// NewStaticHandler serves the browser client from files (the root of the
// build) on the same origin as the API (C13-05, ADR 0022). Mount it last:
// it answers every path the API routes do not.
func NewStaticHandler(files fs.FS, logger *slog.Logger) http.Handler {
	if files == nil || logger == nil {
		panic("nil static files or logger")
	}
	return staticHandler{files: files, logger: logger}
}

type staticHandler struct {
	files  fs.FS
	logger *slog.Logger
}

func (h staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isReserved(r.URL.Path) {
		httperr.RespondWithSlugError(w, r, h.logger, errNotFound)
		return
	}
	header := w.Header()
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, `{"slug":"method-not-allowed","message":"the browser client answers GET and HEAD only"}`+"\n")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if info, err := fs.Stat(h.files, name); !fs.ValidPath(name) || name == "" || err != nil || info.IsDir() {
		name = "index.html" // client-side routes
	}
	if strings.HasPrefix(name, "assets/") {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "no-cache")
	}
	if name == "index.html" {
		// ServeFileFS would redirect a request for /index.html to /.
		serveIndex(w, r, h.files)
		return
	}
	http.ServeFileFS(w, r, h.files, name) //nolint:gosec // name is cleaned, fs.ValidPath and found in the embedded build; fs.FS cannot leave its root

}

func serveIndex(w http.ResponseWriter, r *http.Request, files fs.FS) {
	file, err := files.Open("index.html")
	if err != nil {
		http.Error(w, "the browser client is missing", http.StatusNotFound)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "the browser client is missing", http.StatusNotFound)
		return
	}
	content, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "the browser client is missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", info.ModTime(), content)
}

func isReserved(urlPath string) bool {
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(urlPath, prefix) {
			return true
		}
	}
	for _, p := range reservedPaths {
		if urlPath == p {
			return true
		}
	}
	return false
}
