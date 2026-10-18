package remoteitem

import (
	"errors"
	"time"
)

// Job is a user's claim on a Remote Item (spec §2), an entity inside the
// aggregate (ADR 0004).
type Job struct {
	id            JobID
	ownerID       string
	ownerUsername string
	displayName   string
	claimedAt     time.Time
	origin        Origin
}

// NewJob builds a job before it is added by StartSubmission or Claim.
func NewJob(id JobID, ownerID, ownerUsername, displayName string, claimedAt time.Time) (Job, error) {
	switch {
	case id.IsZero():
		return Job{}, errors.New("empty job ID")
	case ownerID == "":
		return Job{}, errors.New("empty job owner")
	case claimedAt.IsZero():
		return Job{}, errors.New("zero claim time")
	}
	displayName, err := NormalizeDisplayName(displayName)
	if err != nil {
		return Job{}, err
	}
	return Job{id: id, ownerID: ownerID, ownerUsername: ownerUsername, displayName: displayName, claimedAt: claimedAt, origin: OriginJoined}, nil
}

// UnmarshalJobFromDatabase must only be used by repositories. It is not a constructor.
func UnmarshalJobFromDatabase(id JobID, ownerID, ownerUsername, displayName string, claimedAt time.Time, origin Origin) (Job, error) {
	job, err := NewJob(id, ownerID, ownerUsername, displayName, claimedAt)
	if err != nil {
		return Job{}, err
	}
	job.origin = origin
	return job, nil
}

func (j Job) ID() JobID             { return j.id }
func (j Job) OwnerID() string       { return j.ownerID }
func (j Job) OwnerUsername() string { return j.ownerUsername }
func (j Job) DisplayName() string   { return j.displayName }
func (j Job) ClaimedAt() time.Time  { return j.claimedAt }
func (j Job) Origin() Origin        { return j.origin }

// Submission is the state of a create in flight (spec §5.1, ADR 0005).
type Submission struct {
	StartedAt time.Time
	// AttemptStartedAt is zero while no attempt is in progress.
	AttemptStartedAt time.Time
	Attempts         int
	// NotBefore is the earliest next attempt after a budget block; zero if none.
	NotBefore      time.Time
	FollowUpsDue   []time.Time
	DeferredBehind ID
	// Cached: SubmitJob's cached check reported the hash cached; every
	// attempt uses the cheaper create class (ADR 0013 rule 4, DEC-49).
	Cached bool
}

func (s Submission) HasAttemptInProgress() bool { return !s.AttemptStartedAt.IsZero() }
