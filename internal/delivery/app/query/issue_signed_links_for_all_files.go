package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
)

type issueSignedLinksForAllFilesHandler struct {
	targets out.JobTargets
	linker  linker
}

func NewIssueSignedLinksForAllFilesHandler(targets out.JobTargets, config LinkConfig, clk clock.Clock, logger *slog.Logger, metricsClient decorator.MetricsClient) (in.IssueSignedLinksForAllFilesHandler, error) {
	if targets == nil {
		panic("nil jobTargets")
	}
	l, err := newLinker(config, clk)
	if err != nil {
		return nil, err
	}
	return decorator.ApplyQueryDecorators[in.IssueSignedLinksForAllFiles, []in.SignedLinkResult](
		issueSignedLinksForAllFilesHandler{targets: targets, linker: l}, logger, metricsClient), nil
}

func (h issueSignedLinksForAllFilesHandler) Handle(ctx context.Context, query in.IssueSignedLinksForAllFiles) ([]in.SignedLinkResult, error) {
	userID, err := actorID(query.Actor)
	if err != nil {
		return nil, err
	}
	targets, err := h.targets.AllTargets(ctx, userID, query.JobID)
	if err != nil {
		return nil, err
	}
	links := make([]in.SignedLinkResult, 0, len(targets))
	for _, target := range targets {
		link, err := h.linker.link(userID, query.JobID, target)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}
