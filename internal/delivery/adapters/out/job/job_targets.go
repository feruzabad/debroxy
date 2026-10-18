// Package job is delivery's adapter to the job context (DEP-4, OUT-5): it
// resolves delivery targets through job's queries, as the job's owner.
package job

import (
	"context"

	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
	jobin "github.com/feruzabad/debroxy/internal/job/ports/in"
)

// JobTargets implements out.JobTargets over job's Application.
type JobTargets struct {
	app jobin.Application
}

var _ out.JobTargets = JobTargets{}

func NewJobTargets(app jobin.Application) JobTargets {
	if app.Queries.DeliveryTarget == nil || app.Queries.JobDetails == nil {
		panic("nil job application")
	}
	return JobTargets{app: app}
}

// owner is the actor delivery uses: links are for the job's owner only,
// so the user acts in the plain user role even when they are an admin.
func owner(userID string) jobin.Actor { return jobin.Actor{UserID: userID, Role: "user"} }

func (t JobTargets) DeliveryTarget(ctx context.Context, userID, jobID string, target out.TargetRequest) (out.Target, error) {
	result, err := t.app.Queries.DeliveryTarget.Handle(ctx, jobin.DeliveryTarget{
		Actor: owner(userID), JobID: jobID, FileID: target.FileID, Bundle: target.Bundle,
	})
	if err != nil {
		return out.Target{}, err // job's slugs: job-not-found, target-not-available
	}
	return out.Target{
		ProviderID: result.ProviderID, Kind: result.Kind, ProviderItemID: result.ProviderItemID,
		FileID: result.FileID, Bundle: result.Bundle, Name: result.Name, RangeSupported: result.RangeSupported,
	}, nil
}

// AllTargets asks job for every file in stored order when the item offers
// per-file delivery, otherwise for the bundle; job checks each target.
func (t JobTargets) AllTargets(ctx context.Context, userID, jobID string) ([]out.Target, error) {
	view, err := t.app.Queries.JobDetails.Handle(ctx, jobin.JobDetails{Actor: owner(userID), JobID: jobID})
	if err != nil {
		return nil, err
	}
	if !view.Delivery.PerFile || len(view.Files) == 0 {
		target, err := t.DeliveryTarget(ctx, userID, jobID, out.TargetRequest{Bundle: true})
		if err != nil {
			return nil, err
		}
		return []out.Target{target}, nil
	}
	targets := make([]out.Target, 0, len(view.Files))
	for _, file := range view.Files {
		target, err := t.DeliveryTarget(ctx, userID, jobID, out.TargetRequest{FileID: file.ID})
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}
