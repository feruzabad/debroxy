package out

import "github.com/feruzabad/debroxy/internal/identity/domain/user"

// PasswordHasher turns passwords into hashes and checks them (argon2id in
// production). It is slow on purpose; callers hash outside transactions.
type PasswordHasher interface {
	Hash(password user.Password) (user.PasswordHash, error)
	// Verify reports whether password matches hash, in constant time.
	Verify(hash user.PasswordHash, password string) (bool, error)
}
