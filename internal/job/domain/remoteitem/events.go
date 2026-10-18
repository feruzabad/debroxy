package remoteitem

// Event is a fact recorded by RemoteItem (D-12). Repositories persist the
// ones with storage effects in the same transaction (R-6): JobReleased
// writes job_history, DeletionRequested writes the outbox message.
type Event interface{ isRemoteItemEvent() }

type LifecycleAdvanced struct {
	From, To State
	Cause    string
}

type SubmissionStarted struct{ JobID JobID }

type SubmissionCompleted struct{ ProviderItemID ProviderItemID }

type SubmissionFailed struct {
	Reason    FailureReason
	Claimants []JobID
}

type SubmissionDeferred struct{ Blocker ID }

type JobClaimed struct{ Job Job }

// JobReleased: FinalState is the item's state at release, never deleting
// or deleted (ADR 0004 rule 6).
type JobReleased struct {
	Job           Job
	FinalState    State
	FailureReason FailureReason
}

type JobRenamed struct {
	JobID JobID
	Name  string
}

type LastClaimReleased struct{}

// ChargeSpreadToClaimants carries other users' IDs: internal to job, never
// exposed (ADR 0006 rule 6, NFR-04).
type ChargeSpreadToClaimants struct{ UserIDs []string }

// DeletionRequested asks the outbox to delete the provider item (ADR 0008).
type DeletionRequested struct{ ProviderItemID ProviderItemID }

type StallDetected struct{}

// SyncAnomalyDetected: an observation asked for a forbidden move; it was
// not applied (DEC-09).
type SyncAnomalyDetected struct{ From, To State }

func (LifecycleAdvanced) isRemoteItemEvent()       {}
func (SubmissionStarted) isRemoteItemEvent()       {}
func (SubmissionCompleted) isRemoteItemEvent()     {}
func (SubmissionFailed) isRemoteItemEvent()        {}
func (SubmissionDeferred) isRemoteItemEvent()      {}
func (JobClaimed) isRemoteItemEvent()              {}
func (JobReleased) isRemoteItemEvent()             {}
func (JobRenamed) isRemoteItemEvent()              {}
func (LastClaimReleased) isRemoteItemEvent()       {}
func (ChargeSpreadToClaimants) isRemoteItemEvent() {}
func (DeletionRequested) isRemoteItemEvent()       {}
func (StallDetected) isRemoteItemEvent()           {}
func (SyncAnomalyDetected) isRemoteItemEvent()     {}
