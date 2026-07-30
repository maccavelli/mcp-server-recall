//go:build windows

package lifecycle

import (
	"io"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

type winLock struct {
	f *os.File
}

func (l *winLock) Close() error {
	h := windows.Handle(l.f.Fd())
	var overlapped windows.Overlapped
	windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
	return l.f.Close()
}

// TryLock attempts to acquire an exclusive lock on the specified path.
func TryLock(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}

	// Use x/sys/windows for exclusive non-blocking lock
	h := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		f.Close()
		return nil, err // Another instance holds the lock
	}

	// Zero-reflection PID write
	f.Truncate(0)
	f.WriteString(strconv.Itoa(os.Getpid()))

	return &winLock{f: f}, nil
}
