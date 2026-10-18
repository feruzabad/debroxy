package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// ExecuteSubmission runs one provider create attempt for a submitting item
// and records its outcome (spec §5.5 steps 4–6). Started by SubmitJob and by
// ResumeDeferredSubmissions.
type ExecuteSubmission struct {
	RemoteItemID string
}

type ExecuteSubmissionHandler decorator.CommandHandler[ExecuteSubmission]
