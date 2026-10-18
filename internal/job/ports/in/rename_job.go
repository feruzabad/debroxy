package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// RenameJob changes the display name of the owner's job.
type RenameJob struct {
	Actor       Actor
	JobID       string
	DisplayName string
}

type RenameJobHandler decorator.CommandHandler[RenameJob]
