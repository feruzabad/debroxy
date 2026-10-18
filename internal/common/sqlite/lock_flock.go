//go:build linux || darwin

package sqlite

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireStartLock takes an exclusive, non-blocking flock on a sidecar file
// and keeps it for the life of the process (ADR 0002 rule 11). The
// operating system releases it when the process exits or crashes.
func acquireStartLock(lockPath string) (*startLock, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path comes from validated configuration
	if err != nil {
		return nil, fmt.Errorf("sqlite: open lock file: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(DatabaseLockedError{LockPath: lockPath}, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("sqlite: lock %s: %w", lockPath, err), closeErr)
	}
	return &startLock{file: file}, nil
}
