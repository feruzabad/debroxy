package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

// Submit classes of the per-user submit rate (DEC-24, ADR 0013 rule 16).
const (
	submitClassUncached = "uncached"
	submitClassCached   = "cached"
)

// SubmissionConfig holds the submission settings of spec §5.5 and §9.4.
type SubmissionConfig struct {
	DefaultSlotQuota        int
	SubmitRatePerHour       int
	CachedSubmitRatePerHour int
	// RequestDeadline is how long SubmitJob waits for the provider.
	RequestDeadline time.Duration
	// ExecutionTimeout bounds one detached attempt, provider call included.
	ExecutionTimeout time.Duration
	// FollowUps are the refreshes after an ambiguous create, relative to it.
	FollowUps []time.Duration
}

// DefaultFollowUps is the schedule of spec §5.6.
var DefaultFollowUps = []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute}

// lateRestarts bounds how often one execution restarts the create after
// the late deletion check fired; a further restart is left to
// ResumeDeferredSubmissions.
const lateRestarts = 3

// recordTimeout bounds the transactions that record an outcome. They run
// even when the execution context ended, so a known outcome is not lost.
const recordTimeout = 10 * time.Second

// discardedError: the definite failure discarded the item; the creator
// learns it synchronously within the request deadline (C6-08).
type discardedError struct{ cause error }

func (e discardedError) Error() string { return "submission discarded: " + e.cause.Error() }
func (e discardedError) Unwrap() error { return e.cause }

// submissionRunner runs ExecuteSubmission's steps 4–6 for SubmitJob and
// for the ExecuteSubmission command.
type submissionRunner struct {
	items    out.RemoteItemRepository
	provider out.ProviderGateway
	clock    clock.Clock
	logger   *slog.Logger
	config   SubmissionConfig
}

// SubmissionDependencies are shared by SubmitJob and ExecuteSubmission.
type SubmissionDependencies struct {
	Items    out.RemoteItemRepository
	Provider out.ProviderGateway
	Clock    clock.Clock
	Config   SubmissionConfig
}

func (d SubmissionDependencies) build(logger *slog.Logger) submissionRunner {
	return newSubmissionRunner(d.Items, d.Provider, d.Clock, logger, d.Config)
}

func newSubmissionRunner(items out.RemoteItemRepository, provider out.ProviderGateway, clk clock.Clock, logger *slog.Logger, config SubmissionConfig) submissionRunner {
	if items == nil {
		panic("nil remoteItemRepository")
	}
	if provider == nil {
		panic("nil providerGateway")
	}
	if clk == nil {
		panic("nil clock")
	}
	if logger == nil {
		panic("nil logger")
	}
	return submissionRunner{items: items, provider: provider, clock: clk, logger: logger, config: config}
}

// execute runs attempts until one has an outcome. started is the item
// whose first attempt StartSubmission just opened (ADR 0005 rule 5), or
// nil when the attempt must be started here. The only error that is an
// outcome is discardedError.
func (r submissionRunner) execute(ctx context.Context, id remoteitem.ID, started *remoteitem.RemoteItem) error {
	for restart := 0; restart <= lateRestarts; restart++ {
		again, err := r.attempt(ctx, id, started)
		if err != nil || !again {
			return err
		}
		started = nil
	}
	return nil
}

// attempt is one provider create; again reports that the late deletion
// check fired and a new attempt must start at once. The submission's
// cached class picks the provider rate class (ADR 0013 rule 4, DEC-49).
func (r submissionRunner) attempt(ctx context.Context, id remoteitem.ID, started *remoteitem.RemoteItem) (again bool, err error) {
	if started == nil {
		if started, err = r.startAttempt(ctx, id); err != nil || started == nil {
			return false, err // another runner owns the attempt, or nothing to do
		}
	}

	submission, _ := started.Submission()
	callCtx, cancel := context.WithTimeout(ctx, r.config.ExecutionTimeout)
	created, createErr := r.provider.CreateRemoteItem(callCtx, started.ProviderID(), started.Source(), submission.Cached)
	cancel()

	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancelRecord()
	var notSent remoteitem.CreateNotSentError
	switch {
	case createErr == nil:
		return r.recordSuccess(recordCtx, id, created)
	case errors.As(createErr, &notSent):
		return false, r.recordInProgress(recordCtx, id, func(item *remoteitem.RemoteItem) error {
			return item.BlockAttempt(r.clock.Now(), notSent.RetryAfter)
		})
	case !remoteitem.IsDefiniteCreateFailure(createErr) || ctx.Err() != nil:
		return false, r.recordInProgress(recordCtx, id, func(item *remoteitem.RemoteItem) error {
			return item.RecordAmbiguousFailure(r.clock.Now(), r.config.FollowUps)
		})
	}
	return false, r.recordDefiniteFailure(recordCtx, id, createErr)
}

// startAttempt starts an attempt only if none is in progress (ADR 0005
// rule 5); it returns nil when this runner must not call the provider.
func (r submissionRunner) startAttempt(ctx context.Context, id remoteitem.ID) (*remoteitem.RemoteItem, error) {
	var started *remoteitem.RemoteItem
	err := r.items.UpdateRemoteItem(ctx, id, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		started = nil
		if item.StartAttempt(r.clock.Now()) {
			started = item
		}
		return item, nil
	})
	return started, err
}

// recordInProgress applies change only while this attempt is still the
// one in progress; otherwise a refresh already settled the item.
func (r submissionRunner) recordInProgress(ctx context.Context, id remoteitem.ID, change func(*remoteitem.RemoteItem) error) error {
	return r.items.UpdateRemoteItem(ctx, id, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		if submission, ok := item.Submission(); !ok || !submission.HasAttemptInProgress() {
			return item, nil
		}
		return item, change(item)
	})
}

// recordDefiniteFailure: only the creator → discard (the item never
// existed for anyone else); otherwise fail (spec §5.5 step 5, C6-08).
func (r submissionRunner) recordDefiniteFailure(ctx context.Context, id remoteitem.ID, cause error) error {
	discarded := false
	reason := remoteitem.NewFailureReason(remoteitem.FailureProviderRejected, failureDetail(cause))
	err := r.items.UpdateRemoteItem(ctx, id, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		if submission, ok := item.Submission(); !ok || !submission.HasAttemptInProgress() {
			return item, nil
		}
		now := r.clock.Now()
		claims := item.Claims()
		if item.State() == remoteitem.StateSubmitting && len(claims) == 1 && claims[0].Origin() == remoteitem.OriginCreated {
			discarded = true
			return item, item.DiscardSubmission(reason, now)
		}
		return item, item.FailSubmission(reason, now)
	})
	if err != nil {
		return err
	}
	if discarded {
		return discardedError{cause: cause}
	}
	return nil
}

func failureDetail(err error) string {
	var rejected remoteitem.ProviderRejectedError
	if errors.As(err, &rejected) {
		return rejected.Detail
	}
	return err.Error()
}

// recordSuccess runs the outcome checks of ADR 0005 rules 7–10 and 15 for
// this runner's attempt, then moves the claims of a merged item.
func (r submissionRunner) recordSuccess(ctx context.Context, id remoteitem.ID, created out.CreatedRemoteItem) (again bool, err error) {
	return r.applyOutcome(ctx, id, created, false)
}

// applyOutcome records a provider item ID found for a submission: from
// this runner's create, or from a refresh listing (spec §5.6 step 3,
// ADR 0005 rule 13). A refresh applies it whether or not an attempt is in
// progress and never restarts the create itself: a fired late deletion
// check leaves the item to ResumeDeferredSubmissions.
func (r submissionRunner) applyOutcome(ctx context.Context, id remoteitem.ID, created out.CreatedRemoteItem, fromRefresh bool) (again bool, err error) {
	var (
		mergeTarget remoteitem.ID
		claims      []remoteitem.Job
	)
	err = r.items.UpdateRemoteItem(ctx, id, func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		again, mergeTarget, claims = false, remoteitem.ID{}, nil
		submission, ok := item.Submission()
		if !ok || (!fromRefresh && !submission.HasAttemptInProgress()) {
			return item, nil // settled meanwhile
		}
		holders, err := reads.ItemsHoldingProviderItem(ctx, item.ProviderID(), created.ProviderItemID)
		if err != nil {
			return nil, err
		}
		since := submission.AttemptStartedAt
		if since.IsZero() {
			since = submission.StartedAt
		}
		decision := decideOutcome(item.ID(), holders, since)
		now := r.clock.Now()
		item.CancelFollowUps()
		if item.State() != remoteitem.StateSubmitting { // released while submitting (rule 15)
			if decision.lateDeletion {
				return item, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureSubmissionLost, "the content was deleted at the provider"), now)
			}
			return item, item.CompleteSubmission(created.ProviderItemID, created.Observation, now)
		}
		switch {
		case !decision.claimable.IsZero():
			mergeTarget, claims = decision.claimable, item.Claims()
			return item, item.FailSubmission(remoteitem.NewFailureReason(remoteitem.FailureMergedInto, decision.claimable.String()), now)
		case !decision.deleting.IsZero():
			return item, item.DeferSubmission(decision.deleting, now)
		case decision.lateDeletion:
			again = !fromRefresh
			return item, item.EndAttempt()
		}
		return item, item.CompleteSubmission(created.ProviderItemID, created.Observation, now)
	})
	if err != nil || mergeTarget.IsZero() {
		return again, err
	}
	return false, r.moveClaims(ctx, id, mergeTarget, claims)
}

type outcomeDecision struct {
	claimable    remoteitem.ID
	deleting     remoteitem.ID
	lateDeletion bool
}

// decideOutcome reads the other items holding the provider item ID: a
// claimable holder (rule 7), a deleting holder (rule 8), or a holder
// deleted after this attempt started (rule 9).
func decideOutcome(self remoteitem.ID, holders []out.ItemHolder, attemptStartedAt time.Time) outcomeDecision {
	var decision outcomeDecision
	for _, holder := range holders {
		switch {
		case holder.ID == self:
		case holder.State.IsClaimable():
			decision.claimable = holder.ID
		case holder.State == remoteitem.StateDeleting:
			decision.deleting = holder.ID
		case holder.State == remoteitem.StateDeleted && !holder.DeletedAt.Before(attemptStartedAt):
			decision.lateDeletion = true
		}
	}
	return decision
}

// moveClaims hands each claim of the merged item over to the target, one
// transaction per item change (DEC-36). jobs.id is unique across items, so
// a claim leaves the merged item before it joins the target; a refused
// claim goes to its owner's history (C6-09).
func (r submissionRunner) moveClaims(ctx context.Context, from, to remoteitem.ID, claims []remoteitem.Job) error {
	var errs []error
	for _, job := range claims {
		if err := r.moveClaim(ctx, from, to, job); err != nil {
			errs = append(errs, fmt.Errorf("move job %s: %w", job.ID(), err))
		}
	}
	return errors.Join(errs...)
}

func (r submissionRunner) moveClaim(ctx context.Context, from, to remoteitem.ID, job remoteitem.Job) error {
	handedOver := false
	err := r.items.UpdateRemoteItem(ctx, from, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		if _, ok := item.Job(job.ID()); !ok {
			return item, nil // moved by an earlier run
		}
		handedOver = true
		_, err := item.HandOverClaim(job.ID(), r.clock.Now())
		return item, err
	})
	if err != nil || !handedOver {
		return err
	}

	refusal := ""
	err = r.items.UpdateRemoteItem(ctx, to, func(ctx context.Context, item *remoteitem.RemoteItem, reads out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		refusal = ""
		if item.NewClaimIsCharged() {
			if err := r.allowsNewCharge(ctx, reads, job.OwnerID()); err != nil {
				refusal = refusalOf(err)
				return item, nil
			}
		}
		switch err := item.Claim(job, r.clock.Now()); {
		case err == nil, errors.Is(err, remoteitem.ErrDuplicateJobID):
		case refusalOf(err) != "":
			refusal = refusalOf(err)
		default:
			return nil, err
		}
		return item, nil
	})
	if err != nil {
		return err
	}
	if refusal == "" {
		return nil
	}
	return r.items.UpdateRemoteItem(ctx, from, func(_ context.Context, item *remoteitem.RemoteItem, _ out.RemoteItemReads) (*remoteitem.RemoteItem, error) {
		item.RecordRefusedHandOver(job, remoteitem.NewFailureReason(remoteitem.FailureMergedRefused, refusal))
		return item, nil
	})
}

// refusalOf names why the target refused a moved claim, or "" when err is
// not a refusal.
func refusalOf(err error) string {
	var exists remoteitem.JobAlreadyExistsError
	var quota slotquota.QuotaExceededError
	switch {
	case errors.As(err, &exists):
		return exists.Slug()
	case errors.As(err, &quota):
		return quota.Slug()
	case errors.Is(err, remoteitem.ErrRemoteItemUnavailable):
		return "remote-item-unavailable"
	}
	return ""
}

// allowsNewCharge checks the user's slot quota inside the transaction (ADR 0006 rule 10).
func (r submissionRunner) allowsNewCharge(ctx context.Context, reads out.RemoteItemReads, userID string) error {
	quota, found, err := reads.SlotQuota(ctx, userID)
	if err != nil {
		return err
	}
	if !found {
		if quota, err = slotquota.NewSlotQuota(userID, r.config.DefaultSlotQuota); err != nil {
			return err
		}
	}
	used, err := reads.ActiveChargedItemCount(ctx, userID)
	if err != nil {
		return err
	}
	return quota.AllowsNewCharge(used)
}
