//go:build !(linux || darwin)

package sqlite

import (
	"fmt"
	"runtime"
)

// acquireStartLock: debroxy supports Linux and macOS only (NFR-05).
func acquireStartLock(string) (*startLock, error) {
	return nil, fmt.Errorf("sqlite: start lock is not supported on %s", runtime.GOOS)
}
