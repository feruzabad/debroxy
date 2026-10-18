package argon2_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/identity/adapters/out/argon2"
	"github.com/feruzabad/debroxy/internal/identity/domain/user"
)

var cheapParams = argon2.Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltBytes: 16, KeyBytes: 32}

func TestArgon2idPasswordHasher_round_trip(t *testing.T) {
	t.Parallel()
	hasher, err := argon2.NewArgon2idPasswordHasher(cheapParams)
	require.NoError(t, err)
	password, err := user.NewPassword("correct horse battery")
	require.NoError(t, err)

	hash, err := hasher.Hash(password)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(hash.String(), "$argon2id$v=19$m=64,t=1,p=1$"))
	assert.NotContains(t, hash.String(), "correct horse")
	ok, err := hasher.Verify(hash, "correct horse battery")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = hasher.Verify(hash, "correct horse batterY")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestArgon2idPasswordHasher_salts_every_hash(t *testing.T) {
	t.Parallel()
	hasher, err := argon2.NewArgon2idPasswordHasher(cheapParams)
	require.NoError(t, err)
	password, err := user.NewPassword("correct horse battery")
	require.NoError(t, err)

	first, err := hasher.Hash(password)
	require.NoError(t, err)
	second, err := hasher.Hash(password)
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestArgon2idPasswordHasher_verifies_hashes_made_with_other_parameters(t *testing.T) {
	t.Parallel()
	old, err := argon2.NewArgon2idPasswordHasher(argon2.Params{MemoryKiB: 128, Iterations: 2, Parallelism: 1, SaltBytes: 16, KeyBytes: 32})
	require.NoError(t, err)
	current, err := argon2.NewArgon2idPasswordHasher(cheapParams)
	require.NoError(t, err)
	password, err := user.NewPassword("correct horse battery")
	require.NoError(t, err)
	hash, err := old.Hash(password)
	require.NoError(t, err)

	ok, err := current.Verify(hash, "correct horse battery")

	require.NoError(t, err)
	assert.True(t, ok)
}

func TestArgon2idPasswordHasher_rejects_malformed_hashes(t *testing.T) {
	t.Parallel()
	hasher, err := argon2.NewArgon2idPasswordHasher(cheapParams)
	require.NoError(t, err)

	for _, encoded := range []string{"plain", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=18$m=64,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$c2FsdA$a2V5"} {
		hash, err := user.NewPasswordHash(encoded)
		require.NoError(t, err)
		_, err = hasher.Verify(hash, "anything")
		assert.Error(t, err, encoded)
	}
}

func TestNewArgon2idPasswordHasher_rejects_weak_parameters(t *testing.T) {
	t.Parallel()

	_, err := argon2.NewArgon2idPasswordHasher(argon2.Params{MemoryKiB: 64, Iterations: 0, Parallelism: 1, SaltBytes: 16, KeyBytes: 32})

	assert.Error(t, err)
}
