// Package id generates identifiers (DEC-19: UUIDv7). Callers generate IDs
// before dispatching a command, so commands stay retryable (ddd-rules A-3).
package id

import "uuid"

// New returns a new UUIDv7 in its canonical string form.
func New() string {
	return uuid.NewV7().String()
}
