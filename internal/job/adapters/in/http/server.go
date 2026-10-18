// Package http is the job HTTP adapter: the generated server for
// api/openapi/job.yml (without the webhook, see ../webhook) plus the
// mapping to job commands and queries (ddd-rules IN-1, IN-2).
package http

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/feruzabad/debroxy/internal/common/auth"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/common/server/httperr"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

var errAuthenticationRequired = commonerrors.NewAuthenticationError("this operation needs a logged-in user", "authentication-required")

// jobIDCarrier is satisfied by errors that name an existing job; the adapter
// uses it to set the Location header on job-already-exists (C14-04) without
// importing the domain.
type jobIDCarrier interface {
	error
	JobID() string
}

// staleSlug marks a refresh that could not give fresh state; reads then
// answer from stored state with stale: true (C5-07).
const staleSlug = "provider-state-stale"

// Config holds the adapter's limits.
type Config struct {
	// UploadLimitBytes bounds an uploaded .torrent or .nzb file (spec §5.5).
	UploadLimitBytes int64
	// EventPingInterval and EventSessionCheckInterval default to 25 s and
	// 60 s (spec §5.11); tests shorten them.
	EventPingInterval         time.Duration
	EventSessionCheckInterval time.Duration
}

// bodyOverhead leaves room for base64 and the JSON around an upload.
const bodyOverhead = 64 << 10

// RegisterRoutes adds every job route except the webhook to mux.
func RegisterRoutes(mux *http.ServeMux, app in.Application, cfg Config, logger *slog.Logger) {
	tooLarge := commonerrors.NewIncorrectInputError("the upload is larger than the limit", "upload-too-large")
	respondWithError := func(w http.ResponseWriter, r *http.Request, err error) {
		var exists jobIDCarrier
		if errors.As(err, &exists) {
			w.Header().Set("Location", "/api/v1/jobs/"+exists.JobID())
		}
		httperr.RespondWithSlugError(w, r, logger, err)
	}
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			respondWithError(w, r, tooLarge)
			return
		}
		respondWithError(w, r, commonerrors.NewIncorrectInputError(err.Error(), "invalid-request"))
	}
	limitBody := func(next http.Handler) http.Handler {
		limit := cfg.UploadLimitBytes*4/3 + bodyOverhead
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
	strict := NewStrictHandlerWithOptions(httpServer{app: app, cfg: cfg, tooLarge: tooLarge, logger: logger}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		ResponseErrorHandlerFunc: respondWithError,
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: badRequest, Middlewares: []MiddlewareFunc{limitBody}})
	events := eventStream{
		app: app, logger: logger, respondWithError: respondWithError,
		pingInterval: cmp.Or(cfg.EventPingInterval, defaultPingInterval), sessionCheckInterval: cmp.Or(cfg.EventSessionCheckInterval, defaultSessionCheckInterval),
	}
	mux.Handle("GET /api/v1/events", events)
}

type httpServer struct {
	app      in.Application
	cfg      Config
	tooLarge error
	logger   *slog.Logger
}

var _ StrictServerInterface = httpServer{}

// actorFromContext converts the principal set by the auth middleware
// (DEP-6). Every protected operation starts here (S1-F3).
func actorFromContext(ctx context.Context) (in.Actor, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return in.Actor{}, errAuthenticationRequired
	}
	return in.Actor{UserID: principal.UserID, Role: principal.Role}, nil
}

func isStale(err error) bool {
	var slugErr commonerrors.SlugError
	return errors.As(err, &slugErr) && slugErr.Slug() == staleSlug
}

// refreshOnRead refreshes each provider before a read (ADR 0009 rule 1).
// A provider that cannot give fresh state makes the answer stale; the read
// itself still succeeds.
func (s httpServer) refreshOnRead(ctx context.Context, actor in.Actor, providers []string) bool {
	stale := false
	for _, provider := range providers {
		err := s.app.Commands.RefreshProviderState.Handle(ctx, in.RefreshProviderState{Actor: actor, ProviderID: provider, Cause: in.RefreshCauseRead})
		if err != nil {
			stale = true
			if !isStale(err) {
				s.logger.WarnContext(ctx, "refresh on read failed", "provider", provider, "error", err.Error())
			}
		}
	}
	return stale
}

func providersOf(views []in.JobView) []string {
	var providers []string
	for _, view := range views {
		if !slices.Contains(providers, view.ProviderID) {
			providers = append(providers, view.ProviderID)
		}
	}
	return providers
}

func (s httpServer) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	user, err := s.app.Queries.CurrentUser.Handle(ctx, in.CurrentUser{Actor: actor})
	if err != nil {
		return nil, err
	}
	quota, err := s.app.Queries.QuotaUsage.Handle(ctx, in.QuotaUsage{Actor: actor})
	if err != nil {
		return nil, err
	}
	submitRate, err := s.app.Queries.SubmitRate.Handle(ctx, in.SubmitRate{Actor: actor})
	if err != nil {
		return nil, err
	}
	return GetMe200JSONResponse{
		User: User{
			Id: user.ID, Username: user.Username, Role: UserRole(user.Role), Status: UserStatus(user.Status),
			PasswordLogin: user.PasswordLogin, OidcLinked: user.OIDCLinked, CreatedAt: user.CreatedAt,
		},
		Quota:   QuotaUsage{SlotsUsed: quota.SlotsUsed, MaxActiveJobs: quota.MaxActiveJobs},
		Session: sessionToResponse(ctx),
		SubmitRate: SubmitRate{
			Uncached: submitRateClassToResponse(submitRate.Uncached),
			Cached:   submitRateClassToResponse(submitRate.Cached),
		},
	}, nil
}

// sessionToResponse reads the session expiry times the auth middleware put
// into the principal (DEP-6): they never enter the application layer.
func sessionToResponse(ctx context.Context) SessionExpiry {
	session := SessionExpiry{}
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		session.IdleExpiresAt = principal.SessionIdleExpiresAt
		session.AbsoluteExpiresAt = principal.SessionAbsoluteExpiresAt
	}
	return session
}

func submitRateClassToResponse(class in.SubmitRateClassResult) SubmitRateClass {
	return SubmitRateClass{Used: class.Used, PerHour: class.PerHour, ResetsAt: class.ResetsAt}
}

func (s httpServer) ListProviders(ctx context.Context, _ ListProvidersRequestObject) (ListProvidersResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := s.app.Queries.Providers.Handle(ctx, in.Providers{Actor: actor})
	if err != nil {
		return nil, err
	}
	response := ProviderList{Providers: make([]Provider, 0, len(providers))}
	for _, p := range providers {
		provider := Provider{Id: p.ID, Kind: p.Kind, DisplayName: p.DisplayName, Enabled: p.Enabled, Capabilities: make([]Capability, 0, len(p.Capabilities))}
		for _, c := range p.Capabilities {
			capability := Capability{Name: c.Name, Supported: c.Supported, Enabled: c.Enabled}
			if c.State != "" {
				state := CapabilityState(c.State)
				capability.State = &state
			}
			provider.Capabilities = append(provider.Capabilities, capability)
		}
		response.Providers = append(response.Providers, provider)
	}
	return ListProviders200JSONResponse(response), nil
}

func (s httpServer) ListHosters(ctx context.Context, request ListHostersRequestObject) (ListHostersResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	hosters, err := s.app.Queries.ProviderHosters.Handle(ctx, in.ProviderHosters{Actor: actor, ProviderID: request.ProviderId})
	if err != nil {
		return nil, err
	}
	response := HosterList{Hosters: make([]Hoster, 0, len(hosters))}
	for _, h := range hosters {
		response.Hosters = append(response.Hosters, Hoster{Name: h.Name, Domains: h.Domains, Up: h.Up, DailyLimit: h.DailyLimit, DailyUsed: h.DailyUsed})
	}
	return ListHosters200JSONResponse(response), nil
}

func (s httpServer) CheckCachedAvailability(ctx context.Context, request CheckCachedAvailabilityRequestObject) (CheckCachedAvailabilityResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.app.Queries.CachedAvailability.Handle(ctx, in.CachedAvailability{
		Actor: actor, ProviderID: request.ProviderId, Fingerprints: request.Body.Fingerprints,
	})
	if err != nil {
		return nil, err
	}
	return CheckCachedAvailability200JSONResponse{Cached: result.Cached}, nil
}

func (s httpServer) RefreshProvider(ctx context.Context, request RefreshProviderRequestObject) (RefreshProviderResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.RefreshProviderState.Handle(ctx, in.RefreshProviderState{Actor: actor, ProviderID: request.ProviderId, Cause: in.RefreshCauseForced})
	if err != nil && !isStale(err) {
		return nil, err
	}
	return RefreshProvider200JSONResponse{Stale: err != nil}, nil
}

func (s httpServer) ListJobs(ctx context.Context, _ ListJobsRequestObject) (ListJobsResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	query := in.JobsOfUser{Actor: actor}
	views, err := s.app.Queries.JobsOfUser.Handle(ctx, query)
	if err != nil {
		return nil, err
	}
	stale := s.refreshOnRead(ctx, actor, providersOf(views))
	if views, err = s.app.Queries.JobsOfUser.Handle(ctx, query); err != nil {
		return nil, err
	}
	return ListJobs200JSONResponse(jobListToResponse(views, stale, false)), nil
}

func (s httpServer) ListAllJobs(ctx context.Context, _ ListAllJobsRequestObject) (ListAllJobsResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	query := in.AllJobs{Actor: actor}
	views, err := s.app.Queries.AllJobs.Handle(ctx, query) // also checks the admin role
	if err != nil {
		return nil, err
	}
	stale := s.refreshOnRead(ctx, actor, providersOf(views))
	if views, err = s.app.Queries.AllJobs.Handle(ctx, query); err != nil {
		return nil, err
	}
	return ListAllJobs200JSONResponse(jobListToResponse(views, stale, true)), nil
}

func (s httpServer) GetJob(ctx context.Context, request GetJobRequestObject) (GetJobResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	query := in.JobDetails{Actor: actor, JobID: request.JobId}
	view, err := s.app.Queries.JobDetails.Handle(ctx, query)
	if err != nil {
		return nil, err
	}
	stale := s.refreshOnRead(ctx, actor, []string{view.ProviderID})
	if view, err = s.app.Queries.JobDetails.Handle(ctx, query); err != nil {
		return nil, err
	}
	job := jobViewToResponse(view, actor.Role == "admin")
	job.Stale = &stale
	return GetJob200JSONResponse(job), nil
}

// SubmitJob runs the command, then reads the job back (A-3): 202 while the
// item is still submitting, 201 otherwise, and 200 with the history entry
// for a retry of a job released since (DEC-38).
func (s httpServer) SubmitJob(ctx context.Context, request SubmitJobRequestObject) (SubmitJobResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	source, err := s.sourceToCommand(request.Body.Source)
	if err != nil {
		return nil, err
	}
	jobID := id.New()
	if request.Body.Id != nil {
		jobID = *request.Body.Id
	}
	cmd := in.SubmitJob{Actor: actor, JobID: jobID, ProviderID: request.Body.ProviderId, Source: source}
	if request.Body.DisplayName != nil {
		cmd.DisplayName = *request.Body.DisplayName
	}
	if err := s.app.Commands.SubmitJob.Handle(ctx, cmd); err != nil {
		return nil, err
	}
	view, err := s.app.Queries.JobDetails.Handle(ctx, in.JobDetails{Actor: actor, JobID: jobID})
	var slugErr commonerrors.SlugError
	switch {
	case err == nil && view.State == "submitting":
		return SubmitJob202JSONResponse{Slug: "submission-pending", Job: jobViewToResponse(view, false)}, nil
	case err == nil:
		return SubmitJob201JSONResponse(jobViewToResponse(view, false)), nil
	case !errors.As(err, &slugErr) || slugErr.Slug() != "job-not-found":
		return nil, err
	}
	history, err := s.app.Queries.JobHistory.Handle(ctx, in.JobHistory{Actor: actor})
	if err != nil {
		return nil, err
	}
	for _, entry := range history {
		if entry.JobID == jobID {
			return SubmitJob200JSONResponse(historyEntryToResponse(entry)), nil
		}
	}
	return nil, err
}

func (s httpServer) sourceToCommand(source Source) (in.Source, error) {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	cmd := in.Source{
		Type: string(source.Type), Magnet: deref(source.Magnet), URL: deref(source.Url),
		Password: deref(source.Password), FileName: deref(source.FileName),
	}
	if source.File != nil {
		if int64(len(*source.File)) > s.cfg.UploadLimitBytes {
			return in.Source{}, s.tooLarge
		}
		cmd.File = *source.File
	}
	return cmd, nil
}

func (s httpServer) ListJobHistory(ctx context.Context, _ ListJobHistoryRequestObject) (ListJobHistoryResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := s.app.Queries.JobHistory.Handle(ctx, in.JobHistory{Actor: actor})
	if err != nil {
		return nil, err
	}
	response := HistoryList{Entries: make([]HistoryEntry, 0, len(entries))}
	for _, entry := range entries {
		response.Entries = append(response.Entries, historyEntryToResponse(entry))
	}
	return ListJobHistory200JSONResponse(response), nil
}

func (s httpServer) RenameJob(ctx context.Context, request RenameJobRequestObject) (RenameJobResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.RenameJob.Handle(ctx, in.RenameJob{Actor: actor, JobID: request.JobId, DisplayName: request.Body.DisplayName}); err != nil {
		return nil, err
	}
	view, err := s.app.Queries.JobDetails.Handle(ctx, in.JobDetails{Actor: actor, JobID: request.JobId})
	if err != nil {
		return nil, err
	}
	return RenameJob200JSONResponse(jobViewToResponse(view, false)), nil
}

func (s httpServer) ReleaseJob(ctx context.Context, request ReleaseJobRequestObject) (ReleaseJobResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.ReleaseJob.Handle(ctx, in.ReleaseJob{Actor: actor, JobID: request.JobId}); err != nil {
		return nil, err
	}
	return ReleaseJob204Response{}, nil
}

// ReleaseAnyJob is ReleaseJob for admins: the domain lets an admin release
// any job, and lets every owner release their own.
func (s httpServer) ReleaseAnyJob(ctx context.Context, request ReleaseAnyJobRequestObject) (ReleaseAnyJobResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.ReleaseJob.Handle(ctx, in.ReleaseJob{Actor: actor, JobID: request.JobId}); err != nil {
		return nil, err
	}
	return ReleaseAnyJob204Response{}, nil
}

func (s httpServer) SetSlotQuota(ctx context.Context, request SetSlotQuotaRequestObject) (SetSlotQuotaResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	err = s.app.Commands.SetSlotQuota.Handle(ctx, in.SetSlotQuota{Actor: actor, UserID: request.UserId, MaxActiveJobs: request.Body.MaxActiveJobs})
	if err != nil {
		return nil, err
	}
	return SetSlotQuota204Response{}, nil
}

func (s httpServer) GetUserSlotQuota(ctx context.Context, request GetUserSlotQuotaRequestObject) (GetUserSlotQuotaResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.app.Queries.SlotQuotaOf.Handle(ctx, in.SlotQuotaOf{Actor: actor, UserID: request.UserId})
	if err != nil {
		return nil, err
	}
	return GetUserSlotQuota200JSONResponse{
		MaxActiveJobs: result.MaxActiveJobs, IsDefault: result.IsDefault, SlotsUsed: result.SlotsUsed,
	}, nil
}

func (s httpServer) ListUnmanagedItems(ctx context.Context, _ ListUnmanagedItemsRequestObject) (ListUnmanagedItemsResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.app.Queries.UnmanagedItems.Handle(ctx, in.UnmanagedItems{Actor: actor})
	if err != nil {
		return nil, err
	}
	response := UnmanagedItemList{Items: make([]UnmanagedItem, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, UnmanagedItem{
			Provider: item.ProviderID, ProviderItemId: item.ProviderItemID, Kind: item.Kind, Name: item.Name,
			SizeBytes: item.SizeBytes, State: item.State, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt,
		})
	}
	return ListUnmanagedItems200JSONResponse(response), nil
}

func (s httpServer) ListParkedOutboxMessages(ctx context.Context, _ ListParkedOutboxMessagesRequestObject) (ListParkedOutboxMessagesResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	messages, err := s.app.Queries.ParkedOutboxMessages.Handle(ctx, in.ParkedOutboxMessages{Actor: actor})
	if err != nil {
		return nil, err
	}
	response := OutboxMessageList{Messages: make([]OutboxMessage, 0, len(messages))}
	for _, m := range messages {
		response.Messages = append(response.Messages, OutboxMessage{
			Id: m.ID, Kind: m.Kind, RemoteItemId: m.RemoteItemID, Attempts: m.Attempts, LastError: m.LastError,
			CreatedAt: m.CreatedAt, ParkedAt: m.ParkedAt,
		})
	}
	return ListParkedOutboxMessages200JSONResponse(response), nil
}

func (s httpServer) ProbeProviderCapabilities(ctx context.Context, request ProbeProviderCapabilitiesRequestObject) (ProbeProviderCapabilitiesResponseObject, error) {
	actor, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.Commands.ProbeProviderCapabilities.Handle(ctx, in.ProbeProviderCapabilities{Actor: actor, ProviderID: request.ProviderId}); err != nil {
		return nil, err
	}
	return ProbeProviderCapabilities204Response{}, nil
}

func jobListToResponse(views []in.JobView, stale, admin bool) JobList {
	list := JobList{Jobs: make([]Job, 0, len(views)), Stale: stale}
	for _, view := range views {
		list.Jobs = append(list.Jobs, jobViewToResponse(view, admin))
	}
	return list
}

// jobViewToResponse maps the representation of spec §10.2. The internal
// fields (fingerprint, remote item and provider item IDs) never leave
// debroxy; the owner's name is shown to admins only.
func jobViewToResponse(view in.JobView, admin bool) Job {
	job := Job{
		Id: view.ID, DisplayName: view.DisplayName, Provider: view.ProviderID, Kind: JobKind(view.Kind), State: JobState(view.State),
		SizeBytes: view.SizeBytes, ExpiresAt: view.ExpiresAt, Shared: view.Shared, SlotCharged: view.SlotCharged,
		CreatedAt: view.CreatedAt, Files: make([]File, 0, len(view.Files)),
		Telemetry: Telemetry{
			Progress: view.Telemetry.Progress, BytesPerSecond: view.Telemetry.BytesPerSecond,
			EtaSeconds: view.Telemetry.ETASeconds, Peers: view.Telemetry.Peers, Stalled: view.Telemetry.Stalled,
		},
		Delivery: Delivery{PerFile: view.Delivery.PerFile},
	}
	if view.Delivery.Bundle != nil {
		job.Delivery.Bundle = &Bundle{Format: BundleFormat(view.Delivery.Bundle.Format), RangeSupported: view.Delivery.Bundle.RangeSupported}
	}
	for _, f := range view.Files {
		job.Files = append(job.Files, File{Id: f.ID, Path: f.Path, SizeBytes: f.SizeBytes})
	}
	if view.FailureReason != "" {
		reason := view.FailureReason
		job.FailureReason = &reason
	}
	if admin {
		owner := view.OwnerUsername
		job.OwnerUsername = &owner
	}
	return job
}

func historyEntryToResponse(entry in.HistoryEntry) HistoryEntry {
	response := HistoryEntry{
		JobId: entry.JobID, DisplayName: entry.DisplayName, Provider: entry.ProviderID, Kind: entry.Kind, Name: entry.Name,
		FinalState: entry.FinalState, ClaimedAt: entry.ClaimedAt.UTC().Truncate(time.Millisecond), ReleasedAt: entry.ReleasedAt.UTC().Truncate(time.Millisecond),
	}
	if entry.FailureReason != "" {
		reason := entry.FailureReason
		response.FailureReason = &reason
	}
	return response
}
