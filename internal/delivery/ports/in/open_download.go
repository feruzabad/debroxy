package in

import (
	"io"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// OpenDownload runs the checks of a download request and opens the
// upstream stream (spec §7.3, ADR 0010 rule 14). The link holder needs no
// session: the token is the credential.
type OpenDownload struct {
	Token string
	// Name is the link's untrusted name segment (ADR 0010 rule 13).
	Name string
	// Method is GET or HEAD.
	Method  string
	Range   string
	IfRange string
}

// Download is the open stream. Headers hold only the whitelisted headers
// (ADR 0010 rule 18). FileName is sanitized for Content-Disposition. The
// caller must close Body: closing ends the upstream request and frees the
// stream slot.
type Download struct {
	Status         int
	Headers        map[string]string
	RangeSupported bool
	FileName       string
	Body           io.ReadCloser
}

type OpenDownloadHandler decorator.QueryHandler[OpenDownload, Download]
