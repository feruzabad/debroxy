// Package remoteitem is the RemoteItem aggregate of the job context
// (spec §5, ADR 0004–0007): the single item at a provider account for one
// fingerprint, the jobs (claims) on it, its submission and its slot charge.
package remoteitem

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// RemoteItem is the consistency boundary for dedup, claims, submission and
// the slot charge (DEC-06). Every flow changes one RemoteItem per
// transaction (D-11); facts about other items reach its methods as plain
// values read by the repository (R-10).
type RemoteItem struct {
	id                ID
	providerID        ProviderID
	providerItemID    ProviderItemID
	providerHash      string
	kind              Kind
	fingerprint       Fingerprint
	source            Source
	name              string
	sizeBytes         int64
	lifecycle         Lifecycle
	failureReason     FailureReason
	files             []RemoteFile
	bundle            *BundleAvailability
	perFileDelivery   bool
	expiresAt         *time.Time
	telemetry         Telemetry
	lastProgressAt    time.Time
	lastObservationAt time.Time
	submission        *Submission
	createdAt         time.Time
	deletedAt         time.Time
	claims            []Job
	chargeHolder      JobID
	chargeReleased    bool
	events            []Event
}

// StartSubmission creates the item in `submitting` before the provider is
// called (DEC-27). The first job becomes the charge holder and the first
// attempt starts now (ADR 0005 rules 3 and 5). cached is the result of
// SubmitJob's cached check, kept for every attempt (DEC-49).
func StartSubmission(id ID, provider ProviderID, source Source, fingerprint Fingerprint, providerHash string, firstJob Job, cached bool, now time.Time) (*RemoteItem, error) {
	switch {
	case id.IsZero():
		return nil, errors.New("empty remote item ID")
	case provider.IsZero():
		return nil, errors.New("empty provider ID")
	case source.IsZero():
		return nil, errors.New("empty source")
	case fingerprint.IsZero():
		return nil, errors.New("empty fingerprint")
	case now.IsZero():
		return nil, errors.New("zero time")
	}
	firstJob.origin = OriginCreated
	r := &RemoteItem{
		id:             id,
		providerID:     provider,
		providerHash:   providerHash,
		kind:           source.Type().Kind(),
		fingerprint:    fingerprint,
		source:         source,
		name:           sourceName(source, firstJob.displayName),
		lifecycle:      Lifecycle{current: StateSubmitting},
		lastProgressAt: now,
		submission:     &Submission{StartedAt: now, AttemptStartedAt: now, Attempts: 1, Cached: cached},
		createdAt:      now,
		claims:         []Job{firstJob},
		chargeHolder:   firstJob.id,
	}
	r.record(SubmissionStarted{JobID: firstJob.id})
	r.record(LifecycleAdvanced{To: StateSubmitting, Cause: "submission-started"})
	return r, nil
}

// sourceName is the initial display name until the provider reports one.
func sourceName(source Source, displayName string) string {
	if displayName != "" {
		return displayName
	}
	switch source.Type() {
	case SourceMagnet:
		if i := strings.Index(source.Magnet(), "dn="); i >= 0 {
			name := source.Magnet()[i+3:]
			if j := strings.IndexByte(name, '&'); j >= 0 {
				name = name[:j]
			}
			return strings.ReplaceAll(name, "+", " ")
		}
	case SourceTorrentFile, SourceNZBFile:
		return source.FileName()
	case SourceWebURL, SourceNZBURL:
		return source.URL()
	}
	return ""
}

// --- reading --------------------------------------------------------------

func (r *RemoteItem) ID() ID                         { return r.id }
func (r *RemoteItem) ProviderID() ProviderID         { return r.providerID }
func (r *RemoteItem) ProviderItemID() ProviderItemID { return r.providerItemID }
func (r *RemoteItem) ProviderHash() string           { return r.providerHash }
func (r *RemoteItem) Kind() Kind                     { return r.kind }
func (r *RemoteItem) Fingerprint() Fingerprint       { return r.fingerprint }
func (r *RemoteItem) Source() Source                 { return r.source }
func (r *RemoteItem) Name() string                   { return r.name }
func (r *RemoteItem) SizeBytes() int64               { return r.sizeBytes }
func (r *RemoteItem) State() State                   { return r.lifecycle.State() }
func (r *RemoteItem) FailureReason() FailureReason   { return r.failureReason }
func (r *RemoteItem) Files() []RemoteFile            { return slices.Clone(r.files) }
func (r *RemoteItem) Bundle() *BundleAvailability    { return r.bundle }
func (r *RemoteItem) PerFileDelivery() bool          { return r.perFileDelivery }
func (r *RemoteItem) ExpiresAt() *time.Time          { return r.expiresAt }
func (r *RemoteItem) Telemetry() Telemetry           { return r.telemetry }
func (r *RemoteItem) LastProgressAt() time.Time      { return r.lastProgressAt }
func (r *RemoteItem) LastObservationAt() time.Time   { return r.lastObservationAt }
func (r *RemoteItem) CreatedAt() time.Time           { return r.createdAt }
func (r *RemoteItem) DeletedAt() time.Time           { return r.deletedAt }
func (r *RemoteItem) Claims() []Job                  { return slices.Clone(r.claims) }
func (r *RemoteItem) ChargeHolder() JobID            { return r.chargeHolder }
func (r *RemoteItem) ChargeHolderReleased() bool     { return r.chargeReleased }
func (r *RemoteItem) Events() []Event                { return r.events }

// ChangedOwners returns the owners of the item's claims before and after
// its recorded events, and the jobs those events released. Claims join only
// through SubmissionStarted (the first job) or JobClaimed, and leave only
// through JobReleased.
func (r *RemoteItem) ChangedOwners() (before, after []string, released []Job) {
	claimed := map[JobID]bool{}
	for _, event := range r.events {
		switch e := event.(type) {
		case SubmissionStarted:
			claimed[e.JobID] = true
		case JobClaimed:
			claimed[e.Job.ID()] = true
		case JobReleased:
			released = append(released, e.Job)
		}
	}
	for _, job := range r.claims {
		after = appendOwner(after, job.OwnerID())
		if !claimed[job.ID()] {
			before = appendOwner(before, job.OwnerID())
		}
	}
	for _, job := range released {
		if !claimed[job.ID()] {
			before = appendOwner(before, job.OwnerID())
		}
	}
	return before, after, released
}

func appendOwner(owners []string, id string) []string {
	if id == "" || slices.Contains(owners, id) {
		return owners
	}
	return append(owners, id)
}
func (r *RemoteItem) ClearEvents()            { r.events = nil }
func (r *RemoteItem) HasProviderItemID() bool { return !r.providerItemID.IsZero() }
func (r *RemoteItem) record(e Event)          { r.events = append(r.events, e) }

// Submission returns a copy of the create state; ok is false when none exists.
func (r *RemoteItem) Submission() (Submission, bool) {
	if r.submission == nil {
		return Submission{}, false
	}
	s := *r.submission
	s.FollowUpsDue = slices.Clone(r.submission.FollowUpsDue)
	return s, true
}

// IsChargedTo: the item is active and the user owns the charge holder, or
// the charge holder was released and the user holds a claim (ADR 0006).
func (r *RemoteItem) IsChargedTo(userID string) bool {
	if !r.State().IsActive() {
		return false
	}
	for _, job := range r.claims {
		if job.ownerID != userID {
			continue
		}
		if job.id == r.chargeHolder || r.chargeReleased {
			return true
		}
	}
	return false
}

// NewClaimIsCharged: a join now would be charged, so the use case checks
// the joiner's quota first (ADR 0006 rule 5).
func (r *RemoteItem) NewClaimIsCharged() bool {
	return r.State().IsActive() && r.chargeReleased
}

// JobOf returns the job a user holds on this item.
func (r *RemoteItem) JobOf(userID string) (Job, bool) {
	for _, job := range r.claims {
		if job.ownerID == userID {
			return job, true
		}
	}
	return Job{}, false
}

// Job returns the job with this ID.
func (r *RemoteItem) Job(id JobID) (Job, bool) {
	for _, job := range r.claims {
		if job.id == id {
			return job, true
		}
	}
	return Job{}, false
}

// --- state changes --------------------------------------------------------

// moveTo is the single place where the state changes. It records the move,
// resets lastProgressAt, and ends the Active Run when leaving the active
// states (ADR 0006 rule 7).
func (r *RemoteItem) moveTo(next State, cause string, now time.Time) error {
	from := r.State()
	lifecycle, err := r.lifecycle.TransitionTo(next)
	if err != nil {
		return err
	}
	if from == next {
		return nil
	}
	r.lifecycle = lifecycle
	r.lastProgressAt = now
	if from.IsActive() && !next.IsActive() {
		r.chargeHolder, r.chargeReleased = JobID{}, false
	}
	if r.telemetry.StallSource == StallSourceDebroxy {
		r.telemetry.Stalled, r.telemetry.StallSource = false, StallSourceNone
	}
	r.record(LifecycleAdvanced{From: from, To: next, Cause: cause})
	return nil
}

func (r *RemoteItem) isReleasedWhileSubmitting() bool {
	return r.State() == StateDeleting && r.providerItemID.IsZero()
}

// Claim adds a joining job (ADR 0004 rule 5). Whether it is charged is
// NewClaimIsCharged; the use case checks the quota before.
func (r *RemoteItem) Claim(job Job, now time.Time) error {
	if !r.State().IsClaimable() {
		return ErrRemoteItemUnavailable
	}
	for _, existing := range r.claims {
		if existing.id == job.id {
			return ErrDuplicateJobID
		}
		if existing.ownerID == job.ownerID {
			return JobAlreadyExistsError{ExistingJobID: existing.id}
		}
	}
	job.origin = OriginJoined
	if job.claimedAt.IsZero() {
		job.claimedAt = now
	}
	r.claims = append(r.claims, job)
	r.record(JobClaimed{Job: job})
	return nil
}

// Release removes a job (ADR 0004 rule 6). The last release requests
// deletion; releasing the charge holder during an Active Run spreads the
// charge to every remaining claimant (ADR 0006 rule 6).
func (r *RemoteItem) Release(jobID JobID, actor Actor, now time.Time) error {
	index := slices.IndexFunc(r.claims, func(j Job) bool { return j.id == jobID })
	if index < 0 || !CanActorSeeJob(actor, r.claims[index]) {
		return JobNotFoundError{JobID: jobID.String()}
	}
	job := r.claims[index]
	r.claims = slices.Delete(r.claims, index, index+1)
	r.record(JobReleased{Job: job, FinalState: r.State()})
	if len(r.claims) == 0 {
		if err := r.requestDeletion(now); err != nil {
			return err
		}
		r.record(LastClaimReleased{})
		return nil
	}
	if job.id == r.chargeHolder && r.State().IsActive() {
		r.chargeHolder, r.chargeReleased = JobID{}, true
		userIDs := make([]string, 0, len(r.claims))
		for _, remaining := range r.claims {
			userIDs = append(userIDs, remaining.ownerID)
		}
		r.record(ChargeSpreadToClaimants{UserIDs: userIDs})
	}
	return nil
}

// requestDeletion follows ADR 0004 rule 12.
func (r *RemoteItem) requestDeletion(now time.Time) error {
	switch state := r.State(); {
	case state == StateSubmitting:
		// The create may still be in flight: submission and source stay.
		return r.moveTo(StateDeleting, "last-claim-released", now)
	case state == StateGone, state == StateFailed && r.providerItemID.IsZero():
		r.deletedAt = now
		return r.moveTo(StateDeleted, "last-claim-released", now)
	case state.IsClaimable(), state == StateFailed:
		if err := r.moveTo(StateDeleting, "last-claim-released", now); err != nil {
			return err
		}
		r.record(DeletionRequested{ProviderItemID: r.providerItemID})
		return nil
	}
	return InvalidOperationError{Operation: "request deletion", State: r.State()}
}

// Rename changes the display name of the owner's job.
func (r *RemoteItem) Rename(jobID JobID, actor Actor, name string) error {
	index := slices.IndexFunc(r.claims, func(j Job) bool { return j.id == jobID })
	if index < 0 || r.claims[index].ownerID != actor.UserID {
		return JobNotFoundError{JobID: jobID.String()}
	}
	name, err := NormalizeDisplayName(name)
	if err != nil {
		return err
	}
	if name == "" {
		return InvalidDisplayNameError{}
	}
	r.claims[index].displayName = name
	r.record(JobRenamed{JobID: jobID, Name: name})
	return nil
}

// --- submission (ADR 0005) -------------------------------------------------

// StartAttempt starts a provider create attempt only if none is in progress
// and NotBefore has passed (ADR 0005 rule 5). It reports whether the caller
// may call the provider.
func (r *RemoteItem) StartAttempt(now time.Time) bool {
	if r.State() != StateSubmitting || r.submission == nil || r.submission.HasAttemptInProgress() {
		return false
	}
	if !r.submission.NotBefore.IsZero() && now.Before(r.submission.NotBefore) {
		return false
	}
	r.submission.AttemptStartedAt = now
	r.submission.Attempts++
	r.submission.NotBefore = time.Time{}
	r.submission.DeferredBehind = ID{}
	return true
}

// BlockAttempt undoes an attempt the rate budget refused before sending
// (ADR 0005 rule 5, ADR 0013 rule 8).
func (r *RemoteItem) BlockAttempt(now time.Time, retryAfter time.Duration) error {
	if r.submission == nil || !r.submission.HasAttemptInProgress() {
		return InvalidOperationError{Operation: "block attempt", State: r.State()}
	}
	r.submission.AttemptStartedAt = time.Time{}
	r.submission.Attempts = max(r.submission.Attempts-1, 0)
	r.submission.NotBefore = now.Add(retryAfter)
	return nil
}

// RecordAmbiguousFailure keeps the attempt in progress and schedules the
// follow-up refreshes (ADR 0005 rule 12).
func (r *RemoteItem) RecordAmbiguousFailure(now time.Time, followUps []time.Duration) error {
	if r.submission == nil {
		return InvalidOperationError{Operation: "record ambiguous failure", State: r.State()}
	}
	r.submission.FollowUpsDue = r.submission.FollowUpsDue[:0]
	for _, after := range followUps {
		r.submission.FollowUpsDue = append(r.submission.FollowUpsDue, now.Add(after))
	}
	return nil
}

// TakeDueFollowUps removes follow-ups due at now and reports how many were due.
func (r *RemoteItem) TakeDueFollowUps(now time.Time) int {
	if r.submission == nil {
		return 0
	}
	kept := r.submission.FollowUpsDue[:0]
	due := 0
	for _, at := range r.submission.FollowUpsDue {
		if at.After(now) {
			kept = append(kept, at)
		} else {
			due++
		}
	}
	r.submission.FollowUpsDue = kept
	return due
}

// EndAttempt ends an attempt whose outcome must not complete, because the
// late deletion check fired (ADR 0005 rule 9, C7-02). The caller starts a
// new attempt at once.
func (r *RemoteItem) EndAttempt() error {
	if r.State() != StateSubmitting || r.submission == nil {
		return InvalidOperationError{Operation: "end attempt", State: r.State()}
	}
	r.submission.AttemptStartedAt = time.Time{}
	return nil
}

// CompleteSubmission records the provider item ID. Precondition: the
// outcome checks of ADR 0005 rules 7–9 passed. A submitting item takes the
// observation's state; an item released while submitting stays deleting
// and asks the outbox to delete the provider item (rule 15).
func (r *RemoteItem) CompleteSubmission(providerItemID ProviderItemID, observation Observation, now time.Time) error {
	if providerItemID.IsZero() {
		return errors.New("empty provider item ID")
	}
	switch {
	case r.State() == StateSubmitting:
		r.providerItemID, r.submission, r.source = providerItemID, nil, Source{}
		target := observation.state
		if target.IsZero() {
			target = StateQueued // unknown provider state: the next refresh tells
		}
		r.applyFields(observation, target != StateSubmitting)
		if err := r.moveTo(target, "submission-completed", now); err != nil {
			return err
		}
		r.lastObservationAt = observation.observedAt
		r.record(SubmissionCompleted{ProviderItemID: providerItemID})
		return nil
	case r.isReleasedWhileSubmitting():
		r.providerItemID, r.submission, r.source = providerItemID, nil, Source{}
		r.record(SubmissionCompleted{ProviderItemID: providerItemID})
		r.record(DeletionRequested{ProviderItemID: providerItemID})
		return nil
	}
	return InvalidOperationError{Operation: "complete submission", State: r.State()}
}

// FailSubmission: a submitting item becomes failed; an item released while
// submitting becomes deleted (spec §5.2).
func (r *RemoteItem) FailSubmission(reason FailureReason, now time.Time) error {
	claimants := make([]JobID, 0, len(r.claims))
	for _, job := range r.claims {
		claimants = append(claimants, job.id)
	}
	switch {
	case r.State() == StateSubmitting:
		r.failureReason, r.submission, r.source = reason, nil, Source{}
		if err := r.moveTo(StateFailed, "submission-failed", now); err != nil {
			return err
		}
	case r.isReleasedWhileSubmitting():
		r.submission, r.source, r.deletedAt = nil, Source{}, now
		if err := r.moveTo(StateDeleted, "submission-failed", now); err != nil {
			return err
		}
	default:
		return InvalidOperationError{Operation: "fail submission", State: r.State()}
	}
	r.record(SubmissionFailed{Reason: reason, Claimants: claimants})
	return nil
}

// DiscardSubmission: a definite failure while only the creator claims the
// item. No provider item exists; the creator's job goes to history as
// failed (C6-08, C7-04).
func (r *RemoteItem) DiscardSubmission(reason FailureReason, now time.Time) error {
	if r.State() != StateSubmitting || len(r.claims) != 1 || r.claims[0].origin != OriginCreated {
		return InvalidOperationError{Operation: "discard submission", State: r.State()}
	}
	creator := r.claims[0]
	r.claims = nil
	r.record(SubmissionFailed{Reason: reason, Claimants: []JobID{creator.id}})
	r.record(JobReleased{Job: creator, FinalState: StateSubmitting, FailureReason: reason})
	r.failureReason, r.submission, r.source, r.deletedAt = reason, nil, Source{}, now
	return r.moveTo(StateDeleted, "submission-discarded", now)
}

// DeferSubmission: a deleting item still holds the returned provider item
// ID; the create runs again after that deletion (ADR 0007 rule 9).
func (r *RemoteItem) DeferSubmission(blocker ID, now time.Time) error {
	if r.State() != StateSubmitting || r.submission == nil {
		return InvalidOperationError{Operation: "defer submission", State: r.State()}
	}
	r.submission.DeferredBehind = blocker
	r.submission.AttemptStartedAt = time.Time{}
	r.record(SubmissionDeferred{Blocker: blocker})
	_ = now
	return nil
}

// --- sync -----------------------------------------------------------------

// Observe applies a provider observation (spec §5.3). Older observations
// are ignored (DEC-09b); a forbidden move is recorded as an anomaly and not
// applied (DEC-09); an unknown state keeps the state but updates the other
// fields.
func (r *RemoteItem) Observe(observation Observation) error {
	if _, observable := syncTransitions[r.State()]; !observable || r.providerItemID.IsZero() {
		return InvalidOperationError{Operation: "observe", State: r.State()}
	}
	if observation.providerItemID != r.providerItemID {
		return fmt.Errorf("observation of %s applied to item holding %s", observation.providerItemID, r.providerItemID)
	}
	if observation.observedAt.Before(r.lastObservationAt) {
		return nil
	}
	now := observation.observedAt
	target := r.State()
	if observation.HasKnownState() && observation.state != r.State() {
		if IsSyncTransition(r.State(), observation.state) {
			target = observation.state
		} else {
			r.record(SyncAnomalyDetected{From: r.State(), To: observation.state})
		}
	}
	r.applyFields(observation, target != r.State())
	if target == StateFailed {
		r.failureReason = NewFailureReason(FailureProviderFailed, observation.failureDetail)
	}
	if err := r.moveTo(target, "observed", now); err != nil {
		return err
	}
	r.lastObservationAt = observation.observedAt
	return nil
}

// applyFields copies observed data. A provider-reported stall is taken as
// reported; a debroxy-detected stall clears on progress or a state change.
func (r *RemoteItem) applyFields(o Observation, stateChanges bool) {
	if o.name != "" {
		r.name = o.name
	}
	if o.sizeBytes > 0 {
		r.sizeBytes = o.sizeBytes
	}
	if len(o.files) > 0 {
		r.files = slices.Clone(o.files)
	}
	if r.providerHash == "" {
		r.providerHash = o.providerHash
	}
	r.bundle, r.perFileDelivery, r.expiresAt = o.bundle, o.perFileDelivery, o.expiresAt
	progressed := o.telemetry.Progress > r.telemetry.Progress
	previous := r.telemetry
	r.telemetry = o.telemetry
	switch {
	case o.telemetry.Stalled:
		r.telemetry.StallSource = StallSourceProvider
	case previous.StallSource == StallSourceDebroxy && !progressed && !stateChanges:
		r.telemetry.Stalled, r.telemetry.StallSource = true, StallSourceDebroxy
	default:
		r.telemetry.StallSource = StallSourceNone
	}
	if progressed {
		r.lastProgressAt = o.observedAt
	}
}

// Revive brings back a failed or gone item whose provider item reappears
// active or ready. The refresh pipeline checks the preconditions of
// ADR 0007 rule 14 through the reads port before calling it. No job is
// added; the charge goes to every claimant while active (ADR 0006 rule 8).
func (r *RemoteItem) Revive(observation Observation, now time.Time) error {
	if (r.State() != StateFailed && r.State() != StateGone) || !observation.IsActiveOrReady() {
		return InvalidOperationError{Operation: "revive", State: r.State()}
	}
	if observation.providerItemID != r.providerItemID {
		return fmt.Errorf("observation of %s cannot revive item holding %s", observation.providerItemID, r.providerItemID)
	}
	r.applyFields(observation, true)
	if err := r.moveTo(observation.state, "revived", now); err != nil {
		return err
	}
	r.failureReason = FailureReason{}
	r.lastObservationAt = observation.observedAt
	r.chargeHolder, r.chargeReleased = JobID{}, observation.state.IsActive()
	return nil
}

// DetectStall flags an item without progress since threshold, only in
// resolving_metadata and downloading, once per run (DEC-35).
func (r *RemoteItem) DetectStall(now time.Time, threshold time.Duration) bool {
	if (r.State() != StateResolvingMetadata && r.State() != StateDownloading) || r.telemetry.Stalled {
		return false
	}
	if now.Sub(r.lastProgressAt) < threshold {
		return false
	}
	r.telemetry.Stalled, r.telemetry.StallSource = true, StallSourceDebroxy
	r.record(StallDetected{})
	return true
}

// MarkGone: the provider item disappeared from a complete listing.
func (r *RemoteItem) MarkGone(now time.Time) error {
	switch state := r.State(); {
	case r.providerItemID.IsZero(), state == StateDeleting, state == StateDeleted, state == StateGone:
		return InvalidOperationError{Operation: "mark gone", State: state}
	}
	return r.moveTo(StateGone, "absent", now)
}

// ConfirmDeleted: the provider deletion is verified (ADR 0008).
func (r *RemoteItem) ConfirmDeleted(now time.Time) error {
	return r.finishDeletion("deletion-confirmed", now)
}

// SupersedeDeletion: a claimable item took over the provider item ID, so no
// provider call happens (ADR 0007 rule 7).
func (r *RemoteItem) SupersedeDeletion(now time.Time) error {
	return r.finishDeletion("deletion-superseded", now)
}

func (r *RemoteItem) finishDeletion(cause string, now time.Time) error {
	if r.State() != StateDeleting {
		return InvalidOperationError{Operation: cause, State: r.State()}
	}
	r.deletedAt = now
	return r.moveTo(StateDeleted, cause, now)
}

// HandOverClaim removes a claim of an item merged into another one
// (DEC-36): the job moves on, so no history is written here. The last
// hand-over finishes the item like a last release.
func (r *RemoteItem) HandOverClaim(jobID JobID, now time.Time) (Job, error) {
	index := slices.IndexFunc(r.claims, func(j Job) bool { return j.id == jobID })
	if index < 0 {
		return Job{}, JobNotFoundError{JobID: jobID.String()}
	}
	job := r.claims[index]
	r.claims = slices.Delete(r.claims, index, index+1)
	if len(r.claims) == 0 {
		if err := r.requestDeletion(now); err != nil {
			return Job{}, err
		}
	}
	return job, nil
}

// RecordRefusedHandOver writes the history of a claim the target item
// refused (C6-09).
func (r *RemoteItem) RecordRefusedHandOver(job Job, reason FailureReason) {
	r.record(JobReleased{Job: job, FinalState: StateFailed, FailureReason: reason})
}

// CancelFollowUps drops the remaining follow-up refreshes once the
// submission's outcome is known (spec §5.6 step 3).
func (r *RemoteItem) CancelFollowUps() {
	if r.submission != nil {
		r.submission.FollowUpsDue = nil
	}
}

// IsWithinAbsenceGrace: the item was created or changed state within the
// grace, so its absence from a listing does not mark it gone yet (spec §5.6
// step 7). lastProgressAt is set on every state change.
func (r *RemoteItem) IsWithinAbsenceGrace(now time.Time, grace time.Duration) bool {
	since := now.Add(-grace)
	return r.createdAt.After(since) || r.lastProgressAt.After(since)
}
