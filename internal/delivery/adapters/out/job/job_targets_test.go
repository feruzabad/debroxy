package job_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/delivery/adapters/out/job"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
	jobin "github.com/feruzabad/debroxy/internal/job/ports/in"
)

type operation[Req, Res any] func(context.Context, Req) (Res, error)

func (o operation[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	return o(ctx, request)
}

func app(view jobin.JobView, asked *[]jobin.DeliveryTarget) jobin.Application {
	return jobin.Application{Queries: jobin.Queries{
		JobDetails: operation[jobin.JobDetails, jobin.JobView](func(context.Context, jobin.JobDetails) (jobin.JobView, error) { return view, nil }),
		DeliveryTarget: operation[jobin.DeliveryTarget, jobin.DeliveryTargetResult](func(_ context.Context, q jobin.DeliveryTarget) (jobin.DeliveryTargetResult, error) {
			*asked = append(*asked, q)
			return jobin.DeliveryTargetResult{FileID: q.FileID, Bundle: q.Bundle}, nil
		}),
	}}
}

func TestAllTargets_lists_files_in_stored_order_as_the_owner(t *testing.T) {
	t.Parallel()
	var asked []jobin.DeliveryTarget
	targets := job.NewJobTargets(app(jobin.JobView{Delivery: jobin.DeliveryView{PerFile: true}, Files: []jobin.FileView{{ID: "9"}, {ID: "2"}}}, &asked))

	all, err := targets.AllTargets(context.Background(), "alice", "job-1")

	require.NoError(t, err)
	assert.Equal(t, []out.Target{{FileID: "9"}, {FileID: "2"}}, all)
	owner := jobin.Actor{UserID: "alice", Role: "user"}
	assert.Equal(t, []jobin.DeliveryTarget{{Actor: owner, JobID: "job-1", FileID: "9"}, {Actor: owner, JobID: "job-1", FileID: "2"}}, asked)
}

func TestAllTargets_falls_back_to_the_bundle(t *testing.T) {
	t.Parallel()
	var asked []jobin.DeliveryTarget
	targets := job.NewJobTargets(app(jobin.JobView{Delivery: jobin.DeliveryView{PerFile: false}, Files: []jobin.FileView{{ID: "1"}, {ID: "2"}}}, &asked))

	all, err := targets.AllTargets(context.Background(), "alice", "job-1")

	require.NoError(t, err)
	assert.Equal(t, []out.Target{{Bundle: true}}, all)
}
