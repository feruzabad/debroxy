package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// IssueSignedLinksForAllFiles returns one link per file in stored order, or
// one bundle link when the item has no per-file delivery (ADR 0011 rule 13).
type IssueSignedLinksForAllFiles struct {
	Actor Actor
	JobID string
}

type IssueSignedLinksForAllFilesHandler decorator.QueryHandler[IssueSignedLinksForAllFiles, []SignedLinkResult]
