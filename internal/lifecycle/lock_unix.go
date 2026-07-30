//go:build !windows

package lifecycle

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

type unixLock struct {
	f *os.File
}

func (l *unixLock) Close() error {
	if err := unix.Flock(int(l.f.Fd()), unix.LOCK_UN); err != nil {
		slog.Debug("failed to unlock process lock", "error", err)
	}
	return l.f.Close()
}

// TryLock attempts to acquire an exclusive lock on the specified path.
func TryLock(path string) (io.Closer, error) {
	path = filepath.Clean(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path is cleaned and caller-controlled lock location
	if err != nil {
		return nil, err
	}

	// Use x/sys/unix for exclusive non-blocking lock
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		_ = f.Close()   //nolint:errcheck // lock acquisition failed; close is best-effort
		return nil, err // Another instance holds the lock
	}

	// Zero-reflection PID write
	if err := f.Truncate(0); err != nil {
		_ = f.Close() //nolint:errcheck // setup failed; close is best-effort
		return nil, err
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = f.Close() //nolint:errcheck // setup failed; close is best-effort
		return nil, err
	}

	return &unixLock{f: f}, nil
}
