package slotquota_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/job/domain/slotquota"
)

func TestSlotQuota_AllowsNewCharge_below_the_limit_only(t *testing.T) {
	t.Parallel()
	quota, err := slotquota.NewSlotQuota("alice", 3)
	require.NoError(t, err)

	assert.NoError(t, quota.AllowsNewCharge(2))
	assert.Equal(t, slotquota.QuotaExceededError{SlotsUsed: 3, MaxActiveJobs: 3}, quota.AllowsNewCharge(3), "equality refuses (ADR 0006 rule 10)")
	_, err = slotquota.NewSlotQuota("alice", -1)
	assert.Error(t, err)
}
