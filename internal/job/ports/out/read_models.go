package out

import (
	"context"
	"time"

	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// JobReadModel answers the job queries (R-8). Jobs of other users are never
// returned to a non-admin; an unknown or foreign job is remoteitem.JobNotFoundError.
type JobReadModel interface {
	JobsOfUser(ctx context.Context, userID string) ([]in.JobView, error)
	AllJobs(ctx context.Context) ([]in.JobView, error)
	// JobsOfItem returns every unreleased job of the item, for any owner;
	// callers apply the policy. An unknown item has no jobs.
	JobsOfItem(ctx context.Context, remoteItemID string) ([]in.JobView, error)
	// JobDetails returns the job without an ownership check; callers apply the policy.
	JobDetails(ctx context.Context, jobID string) (in.JobView, error)
	JobHistory(ctx context.Context, userID string) ([]in.HistoryEntry, error)
	// HistoryEntry finds a released job by ID; found is false when there is none.
	HistoryEntry(ctx context.Context, jobID string) (in.HistoryEntry, bool, error)
	QuotaUsage(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.QuotaUsageResult, error)
	// SlotQuotaOf reports a user's effective quota with the IsDefault flag
	// (C14-01). An unknown user answers the default with SlotsUsed 0.
	SlotQuotaOf(ctx context.Context, userID string, defaultMaxActiveJobs int) (in.SlotQuotaResult, error)
	// SubmitRateUsage counts the user's accepted submits per class within
	// the rolling hour ending at now (ADR 0013 rule 13).
	SubmitRateUsage(ctx context.Context, userID string, now time.Time) (in.SubmitRateUsageResult, error)
}

type UnmanagedItemsReadModel interface {
	UnmanagedItems(ctx context.Context) ([]in.UnmanagedItemView, error)
}

type ParkedOutboxMessagesReadModel interface {
	ParkedOutboxMessages(ctx context.Context) ([]in.OutboxMessageView, error)
}
