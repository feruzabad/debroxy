// Package argon2 implements the PasswordHasher port with argon2id
// (spec §4.1), storing hashes in the PHC string format.
package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/feruzabad/debroxy/internal/identity/domain/user"
)

// Params are the argon2id cost parameters.
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

// DefaultParams follow the OWASP Password Storage Cheat Sheet (argon2id,
// 19 MiB, 2 iterations, 1 lane): about 20–50 ms and 19 MiB per hash.
var DefaultParams = Params{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1, SaltBytes: 16, KeyBytes: 32}

type Argon2idPasswordHasher struct {
	params Params
}

func NewArgon2idPasswordHasher(params Params) (Argon2idPasswordHasher, error) {
	if params.MemoryKiB < 8*uint32(params.Parallelism) || params.Iterations == 0 || params.Parallelism == 0 ||
		params.SaltBytes < 8 || params.KeyBytes < 16 {
		return Argon2idPasswordHasher{}, fmt.Errorf("invalid argon2id parameters %+v", params)
	}
	return Argon2idPasswordHasher{params: params}, nil
}

// Hash returns "$argon2id$v=19$m=<KiB>,t=<iterations>,p=<lanes>$<salt>$<key>".
func (h Argon2idPasswordHasher) Hash(password user.Password) (user.PasswordHash, error) {
	salt := make([]byte, h.params.SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return user.PasswordHash{}, fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password.Plaintext()), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyBytes)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	return user.NewPasswordHash(encoded)
}

// Verify recomputes the key with the parameters stored in hash, so hashes
// made with older parameters keep working.
func (h Argon2idPasswordHasher) Verify(hash user.PasswordHash, password string) (bool, error) {
	params, salt, key, err := decode(hash.String())
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(key))) //nolint:gosec // key length comes from our own encoding
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

var errMalformedHash = errors.New("malformed argon2id hash")

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errMalformedHash
	}
	var params Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.MemoryKiB, &params.Iterations, &params.Parallelism); err != nil {
		return Params{}, nil, nil, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, errMalformedHash
	}
	return params, salt, key, nil
}
