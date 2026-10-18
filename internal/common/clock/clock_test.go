package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

func TestFixed_moves_only_when_told(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	fixed := clock.NewFixed(start)

	assert.Equal(t, start, fixed.Now())
	fixed.Advance(30 * time.Second)
	assert.Equal(t, start.Add(30*time.Second), fixed.Now())
	fixed.Set(start)
	assert.Equal(t, start, fixed.Now())
}

func TestSystem_returns_utc(t *testing.T) {
	t.Parallel()

	assert.Equal(t, time.UTC, clock.System{}.Now().Location())
}
