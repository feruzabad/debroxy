// Package service is the composition root of the delivery context
// (ddd-rules S-1, S-2): it builds the application and its adapters.
package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/delivery/adapters/in/download"
	deliveryhttp "github.com/feruzabad/debroxy/internal/delivery/adapters/in/http"
	"github.com/feruzabad/debroxy/internal/delivery/adapters/out/identity"
	"github.com/feruzabad/debroxy/internal/delivery/adapters/out/job"
	"github.com/feruzabad/debroxy/internal/delivery/adapters/out/provider"
	"github.com/feruzabad/debroxy/internal/delivery/app/query"
	"github.com/feruzabad/debroxy/internal/delivery/domain/signedlink"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	jobin "github.com/feruzabad/debroxy/internal/job/ports/in"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// Config is the delivery configuration, mapped by cmd from the config file.
type Config struct {
	PublicBaseURL        string
	SignedLinkTTL        time.Duration
	MaxStreamsPerUser    int
	MaxConcurrentStreams int
	// SigningKeys: the first key signs, all keys verify (spec §7.1).
	SigningKeys []SigningKey
}

// SigningKey is one Signed Link key.
type SigningKey struct {
	ID     string
	Secret []byte
}

// Dependencies are the shared resources cmd creates (S-1) and the supplier
// contexts' applications (DEP-4).
type Dependencies struct {
	Job      jobin.Application
	Provider providerin.Application
	Identity identityin.Application
	Logger   *slog.Logger
	Metrics  decorator.MetricsClient
	Clock    clock.Clock
}

// NewApplication wires the production adapters. delivery has no storage.
func NewApplication(_ context.Context, cfg Config, deps Dependencies) (in.Application, error) {
	if deps.Logger == nil || deps.Metrics == nil || deps.Clock == nil {
		return in.Application{}, errors.New("delivery: incomplete dependencies")
	}
	return newApplication(cfg, job.NewJobTargets(deps.Job), provider.NewStreamOpener(deps.Provider),
		identity.NewUserStatusChecker(deps.Identity), deps)
}

// NewComponentTestApplication wires the given supplier applications with a
// no-op metrics client (S-2).
func NewComponentTestApplication(cfg Config, jobApp jobin.Application, providerApp providerin.Application, identityApp identityin.Application, clk clock.Clock, logger *slog.Logger) (in.Application, error) {
	return NewApplication(context.Background(), cfg, Dependencies{
		Job: jobApp, Provider: providerApp, Identity: identityApp, Logger: logger, Metrics: decorator.NoOpMetricsClient{}, Clock: clk,
	})
}

func newApplication(cfg Config, targets out.JobTargets, streams out.DeliveryStreamOpener, users out.UserStatusChecker, deps Dependencies) (in.Application, error) {
	keys := make([]signedlink.Key, 0, len(cfg.SigningKeys))
	for _, key := range cfg.SigningKeys {
		keys = append(keys, signedlink.Key{ID: key.ID, Secret: key.Secret})
	}
	links := query.LinkConfig{PublicBaseURL: cfg.PublicBaseURL, TTL: cfg.SignedLinkTTL, Keys: keys}
	logger, metrics, clk := deps.Logger, deps.Metrics, deps.Clock
	issue, err := query.NewIssueSignedLinkHandler(targets, links, clk, logger, metrics)
	if err != nil {
		return in.Application{}, err
	}
	issueAll, err := query.NewIssueSignedLinksForAllFilesHandler(targets, links, clk, logger, metrics)
	if err != nil {
		return in.Application{}, err
	}
	openDownload, err := query.NewOpenDownloadHandler(query.DownloadDependencies{
		Users: users, Slots: query.NewStreamSlots(cfg.MaxStreamsPerUser, cfg.MaxConcurrentStreams), Targets: targets, Streams: streams, Clock: clk,
	}, keys, logger, metrics)
	if err != nil {
		return in.Application{}, err
	}
	return in.Application{Queries: in.Queries{IssueSignedLink: issue, IssueSignedLinksForAllFiles: issueAll, OpenDownload: openDownload}}, nil
}

// RegisterHTTPRoutes mounts the link API and the download routes on mux.
func RegisterHTTPRoutes(mux *http.ServeMux, app in.Application, logger *slog.Logger) {
	deliveryhttp.RegisterRoutes(mux, app, logger)
	download.RegisterRoutes(mux, app.Queries.OpenDownload, logger)
}
