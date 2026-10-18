package in

// Application is everything the job context can be asked to do.
type Application struct {
	Commands Commands
	Queries  Queries
}

type Commands struct {
	SubmitJob         SubmitJobHandler
	ExecuteSubmission ExecuteSubmissionHandler
	ReleaseJob        ReleaseJobHandler
	RenameJob         RenameJobHandler
	SetSlotQuota      SetSlotQuotaHandler

	RefreshProviderState      RefreshProviderStateHandler
	HandleProviderWebhook     HandleProviderWebhookHandler
	ExpireSubmissions         ExpireSubmissionsHandler
	ResumeDeferredSubmissions ResumeDeferredSubmissionsHandler
	DetectStalls              DetectStallsHandler
	RunFollowUpRefreshes      RunFollowUpRefreshesHandler
	RunSafetyRefreshes        RunSafetyRefreshesHandler

	ExecuteRemoteItemDeletion ExecuteRemoteItemDeletionHandler
	ProcessOutbox             ProcessOutboxHandler
	PruneExpiredRecords       PruneExpiredRecordsHandler

	ProbeProviderCapabilities ProbeProviderCapabilitiesHandler
}

type Queries struct {
	JobsOfUser           JobsOfUserHandler
	JobDetails           JobDetailsHandler
	JobHistory           JobHistoryHandler
	AllJobs              AllJobsHandler
	QuotaUsage           QuotaUsageHandler
	SlotQuotaOf          SlotQuotaOfHandler
	SubmitRate           SubmitRateHandler
	WatchJobs            WatchJobsHandler
	CachedAvailability   CachedAvailabilityHandler
	DeliveryTarget       DeliveryTargetHandler
	UnmanagedItems       UnmanagedItemsHandler
	ParkedOutboxMessages ParkedOutboxMessagesHandler
	CurrentUser          CurrentUserHandler
	Providers            ProvidersHandler
	ProviderHosters      ProviderHostersHandler
}
