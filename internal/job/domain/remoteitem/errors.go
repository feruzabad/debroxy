package remoteitem

import (
	"errors"
	"fmt"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

var (
	// ErrRemoteItemUnavailable: the item is not in a claimable state.
	ErrRemoteItemUnavailable = commonerrors.NewConflictError("the item can no longer be claimed; submit it again", "remote-item-unavailable")
	// ErrDuplicateJobID: the JobID is already on the item; the use case answers idempotently.
	ErrDuplicateJobID = errors.New("job ID already present on the item")
)

type UnknownStateError struct{ Given string }

func (e UnknownStateError) Error() string { return fmt.Sprintf("unknown lifecycle state %q", e.Given) }

// InvalidTransitionError: the Lifecycle forbids the move (SM-2).
type InvalidTransitionError struct{ From, To State }

func (e InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid transition from %s to %s", e.From, e.To)
}

// JobAlreadyExistsError: the owner already holds a job on this item (spec §5.3).
type JobAlreadyExistsError struct{ ExistingJobID JobID }

func (e JobAlreadyExistsError) Error() string {
	return fmt.Sprintf("you already have job %s for this content", e.ExistingJobID)
}
func (e JobAlreadyExistsError) Slug() string { return "job-already-exists" }
func (e JobAlreadyExistsError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeConflict
}

// JobID reports the caller's existing job for the same content (C14-04).
func (e JobAlreadyExistsError) JobID() string { return e.ExistingJobID.String() }

// JobNotFoundError is also returned for other users' jobs (NFR-04).
type JobNotFoundError struct{ JobID string }

func (e JobNotFoundError) Error() string                     { return fmt.Sprintf("job %s not found", e.JobID) }
func (e JobNotFoundError) Slug() string                      { return "job-not-found" }
func (e JobNotFoundError) ErrorType() commonerrors.ErrorType { return commonerrors.ErrorTypeNotFound }

type InvalidJobIDError struct{ Given string }

func (e InvalidJobIDError) Error() string { return fmt.Sprintf("invalid job ID %q", e.Given) }
func (e InvalidJobIDError) Slug() string  { return "invalid-request" }
func (e InvalidJobIDError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

// InvalidSourceError: the submitted source cannot be parsed (spec §5.5).
type InvalidSourceError struct{ Reason string }

func (e InvalidSourceError) Error() string { return "invalid source: " + e.Reason }
func (e InvalidSourceError) Slug() string  { return "invalid-source" }
func (e InvalidSourceError) ErrorType() commonerrors.ErrorType {
	return commonerrors.ErrorTypeIncorrectInput
}

// InvalidOperationError: a method was called in a state that does not allow it.
type InvalidOperationError struct {
	Operation string
	State     State
}

func (e InvalidOperationError) Error() string {
	return fmt.Sprintf("%s is not allowed in state %s", e.Operation, e.State)
}

// NotFoundError: no remote item with this ID (R-4).
type NotFoundError struct{ ID string }

func (e NotFoundError) Error() string { return fmt.Sprintf("remote item %s not found", e.ID) }
