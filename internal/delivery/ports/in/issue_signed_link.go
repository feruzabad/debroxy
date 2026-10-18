package in

import (
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// IssueSignedLink signs a link to one target of the owner's job (spec
// §7.2). Set FileID for a file, or Bundle for the provider-built archive.
type IssueSignedLink struct {
	Actor  Actor
	JobID  string
	FileID string
	Bundle bool
}

// SignedLinkResult is one link; URL is the public download URL.
type SignedLinkResult struct {
	URL            string
	ExpiresAt      time.Time
	RangeSupported bool
	FileID         string
	Bundle         bool
}

type IssueSignedLinkHandler decorator.QueryHandler[IssueSignedLink, SignedLinkResult]
