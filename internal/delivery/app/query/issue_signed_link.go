package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
)

type issueSignedLinkHandler struct {
	targets out.JobTargets
	linker  linker
}

func NewIssueSignedLinkHandler(targets out.JobTargets, config LinkConfig, clk clock.Clock, logger *slog.Logger, metricsClient decorator.MetricsClient) (in.IssueSignedLinkHandler, error) {
	if targets == nil {
		panic("nil jobTargets")
	}
	l, err := newLinker(config, clk)
	if err != nil {
		return nil, err
	}
	return decorator.ApplyQueryDecorators[in.IssueSignedLink, in.SignedLinkResult](
		issueSignedLinkHandler{targets: targets, linker: l}, logger, metricsClient), nil
}

// Handle signs a link only for a target job reports as available to the
// owner now (spec §7.2).
func (h issueSignedLinkHandler) Handle(ctx context.Context, query in.IssueSignedLink) (in.SignedLinkResult, error) {
	userID, err := actorID(query.Actor)
	if err != nil {
		return in.SignedLinkResult{}, err
	}
	target, err := h.targets.DeliveryTarget(ctx, userID, query.JobID, out.TargetRequest{FileID: query.FileID, Bundle: query.Bundle})
	if err != nil {
		return in.SignedLinkResult{}, err
	}
	return h.linker.link(userID, query.JobID, target)
}
