package query

import (
	"context"
	"log/slog"

	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
	"github.com/feruzabad/debroxy/internal/job/ports/out"
)

type deliveryTargetHandler struct {
	jobs out.JobReadModel
}

func NewDeliveryTargetHandler(jobs out.JobReadModel, logger *slog.Logger, metricsClient decorator.MetricsClient) in.DeliveryTargetHandler {
	if jobs == nil {
		panic("nil jobReadModel")
	}
	return decorator.ApplyQueryDecorators[in.DeliveryTarget, in.DeliveryTargetResult](deliveryTargetHandler{jobs: jobs}, logger, metricsClient)
}

func (h deliveryTargetHandler) Handle(ctx context.Context, query in.DeliveryTarget) (in.DeliveryTargetResult, error) {
	actor, err := toDomainActor(query.Actor)
	if err != nil {
		return in.DeliveryTargetResult{}, err
	}
	view, err := h.jobs.JobDetails(ctx, query.JobID)
	if err != nil {
		return in.DeliveryTargetResult{}, err
	}
	if view.OwnerID != actor.UserID { // links are for the owner only (spec §10.1)
		return in.DeliveryTargetResult{}, remoteitem.JobNotFoundError{JobID: query.JobID}
	}
	state, err := remoteitem.NewStateFromString(view.State)
	if err != nil {
		return in.DeliveryTargetResult{}, err
	}
	result := in.DeliveryTargetResult{
		ProviderID: view.ProviderID, Kind: view.Kind, ProviderItemID: view.ProviderItemID,
		Bundle: query.Bundle, Name: view.DisplayName,
	}
	fileKnown := false
	for _, file := range view.Files {
		if !query.Bundle && file.ID == query.FileID {
			fileKnown, result.FileID, result.Name = true, file.ID, file.Path
		}
	}
	err = remoteitem.CheckDeliveryTarget(state, view.Delivery.PerFile, fileKnown, view.Delivery.Bundle != nil, query.Bundle)
	if err != nil {
		return in.DeliveryTargetResult{}, err
	}
	if query.Bundle {
		result.RangeSupported = view.Delivery.Bundle.RangeSupported
	} else {
		result.RangeSupported = true
	}
	return result, nil
}
