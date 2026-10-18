package id_test

import (
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/id"
)

func TestNew_returns_time_ordered_uuid_v7(t *testing.T) {
	t.Parallel()

	first, second := id.New(), id.New()

	parsed, err := uuid.Parse(first)
	require.NoError(t, err)
	assert.Equal(t, byte(7), parsed[6]>>4, "version nibble")
	assert.Less(t, first, second, "UUIDv7 strings sort by creation time")
}
