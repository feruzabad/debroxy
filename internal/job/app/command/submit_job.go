package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type submitJobHandler struct {
	runner     submissionRunner
	jobs       out.JobReadModel
	users      out.UserDirectory
	background *Background
}

func NewSubmitJobHandler(
	runner SubmissionDependencies, jobs out.JobReadModel, users out.UserDirectory, background *Background,
	logger *slog.Logger, metricsClient decorator.MetricsClient,
) in.SubmitJobHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	if users == nil {
		panic("nil userDirectory")
	}
	if background == nil {
		panic("nil background")
	}
	return decorator.ApplyCommandDecorators[in.SubmitJob](
		submitJobHandler{runner: runner.build(logger), jobs: jobs, users: users, background: background}, logger, metricsClient)
}

func (h submitJobHandler) Handle(ctx context.Context, cmd in.SubmitJob) error {
	actor, err := toDomainActor(cmd.Actor)
	if err != nil {
		return err
	}
	jobID, err := remoteitem.NewJobID(cmd.JobID)
	if err != nil {
		return err
	}
	providerID, err := remoteitem.NewProviderID(cmd.ProviderID)
	if err != nil {
		return remoteitem.ProviderNotFoundError{Provider: cmd.ProviderID}
	}
	source, err := toDomainSource(cmd.Source)
	if err != nil {
		return err
	}
	fingerprint, providerHash, err := remoteitem.Identify(source)
	if err != nil {
		return err
	}

	// Step 1: idempotency, a plain read before any write (DEC-38).
	if known, err := h.knownJob(ctx, jobID, actor, fingerprint); known || err != nil {
		return err
	}

	// Step 2: validation in the order of ADR 0003 rule 7.
	if err := h.runner.provider.CheckSubmittable(ctx, providerID, source.Type()); err != nil {
		return err
	}
	if source.Type() == remoteitem.SourceWebURL {
		hosters, err := h.runner.provider.HosterCatalogue(ctx, providerID)
		if err != nil {
			return err
		}
		if !remoteitem.MatchesHoster(source.URL(), hosters) {
			return remoteitem.UnsupportedHosterError{Provider: providerID.String()}
		}
	}
	// The cached check is a provider call, so it runs before the
	// transaction (A-9). A failed check counts as uncached (DEC-24).
	class := h.submitClass(ctx, providerID, source, providerHash)

	// Step 3: join or start, one transaction. The job keeps a snapshot of
	// the owner's username (spec §5.1, C9-02).
	owner, err := h.users.User(ctx, actor.UserID)
	if err != nil {
		return err
	}
	job, err := remoteitem.NewJob(jobID, actor.UserID, owner.Username, cmd.DisplayName, h.runner.clock.Now())
	if err != nil {
		return err
	}
	var started *remoteitem.RemoteItem
	err = h.runner.items.JoinOrStart(ctx, providerID, fingerprint,
		func(ctx context.Context, existing *remoteitem.RemoteItem, reads out.RemoteItemReads) (out.JoinOrStartResult, error) {
			started = nil
			now := h.runner.clock.Now()
			if existing != nil {
				if existing.NewClaimIsCharged() {
					if err := h.runner.allowsNewCharge(ctx, reads, actor.UserID); err != nil {
						return out.JoinOrStartResult{}, err
					}
				}
				return out.JoinOrStartResult{Item: existing}, existing.Claim(job, now)
			}
			if err := h.allowsSubmit(ctx, reads, actor.UserID, class, now); err != nil {
				return out.JoinOrStartResult{}, err
			}
			if err := h.runner.allowsNewCharge(ctx, reads, actor.UserID); err != nil {
				return out.JoinOrStartResult{}, err
			}
			itemID, err := remoteitem.NewID(id.New())
			if err != nil {
				return out.JoinOrStartResult{}, err
			}
			item, err := remoteitem.StartSubmission(itemID, providerID, source, fingerprint, providerHash, job, class == submitClassCached, now)
			if err != nil {
				return out.JoinOrStartResult{}, err
			}
			started = item
			return out.JoinOrStartResult{Item: item, SubmitClass: class}, nil
		})
	if errors.Is(err, remoteitem.ErrDuplicateJobID) {
		// A concurrent request with the same JobID won; answer like step 1.
		if known, err := h.knownJob(ctx, jobID, actor, fingerprint); known || err != nil {
			return err
		}
		return ErrJobIDConflict
	}
	if err != nil || started == nil {
		return err // a join never calls the provider
	}
	return h.awaitExecution(ctx, started)
}

// knownJob reports whether the JobID exists as a claim or in history: nil
// for the same owner and content, ErrJobIDConflict otherwise.
func (h submitJobHandler) knownJob(ctx context.Context, jobID remoteitem.JobID, actor remoteitem.Actor, fingerprint remoteitem.Fingerprint) (bool, error) {
	view, err := h.jobs.JobDetails(ctx, jobID.String())
	var notFound remoteitem.JobNotFoundError
	switch {
	case err == nil:
		return true, sameSubmission(view.OwnerID, view.Fingerprint, actor, fingerprint)
	case !errors.As(err, &notFound):
		return false, err
	}
	entry, found, err := h.jobs.HistoryEntry(ctx, jobID.String())
	if err != nil || !found {
		return false, err
	}
	return true, sameSubmission(entry.OwnerID, entry.Fingerprint, actor, fingerprint)
}

func sameSubmission(ownerID, storedFingerprint string, actor remoteitem.Actor, fingerprint remoteitem.Fingerprint) error {
	if ownerID == actor.UserID && storedFingerprint == fingerprint.String() {
		return nil
	}
	return ErrJobIDConflict
}

// submitClass: web submits are always uncached (C6-04); other kinds ask
// the provider when it offers a cached check.
func (h submitJobHandler) submitClass(ctx context.Context, provider remoteitem.ProviderID, source remoteitem.Source, providerHash string) string {
	kind := source.Type().Kind()
	if kind != remoteitem.KindTorrent || providerHash == "" {
		return submitClassUncached
	}
	cached, err := h.runner.provider.CheckCached(ctx, provider, kind, []string{providerHash})
	if err != nil || !cached[providerHash] {
		return submitClassUncached
	}
	return submitClassCached
}

func (h submitJobHandler) allowsSubmit(ctx context.Context, reads out.RemoteItemReads, userID, class string, now time.Time) error {
	limit := h.runner.config.SubmitRatePerHour
	if class == submitClassCached {
		limit = h.runner.config.CachedSubmitRatePerHour
	}
	count, err := reads.SubmitCountSince(ctx, userID, class, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if count >= limit {
		oldest, err := reads.OldestSubmitSince(ctx, userID, class, now.Add(-time.Hour))
		if err != nil {
			return err
		}
		hint := time.Duration(0)
		if !oldest.IsZero() {
			hint = oldest.Add(time.Hour).Sub(now)
		}
		return SubmitRateExceededError{Class: class, PerHour: limit, RetryAfterDuration: hint}
	}
	return nil
}

// awaitExecution runs the create detached from the request and waits at
// most the request deadline (ADR 0005 rule 6). After the deadline the
// outcome is still recorded; the caller answers 202.
func (h submitJobHandler) awaitExecution(ctx context.Context, started *remoteitem.RemoteItem) error {
	itemID := started.ID()
	done := make(chan error, 1)
	running := h.background.Go(func(background context.Context) {
		err := h.runner.execute(background, itemID, started)
		var discarded discardedError
		if err != nil && !errors.As(err, &discarded) {
			h.runner.logger.ErrorContext(background, "submission failed to record", "remote_item_id", itemID.String(), "error", err)
		}
		done <- err
	})
	if !running {
		return nil // shutting down: a refresh or ExpireSubmissions settles the item
	}
	timer := time.NewTimer(h.runner.config.RequestDeadline)
	defer timer.Stop()
	select {
	case err := <-done:
		var discarded discardedError
		if errors.As(err, &discarded) {
			return discarded.cause
		}
		return nil // other errors were logged; the job shows its state
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return nil
	}
}
