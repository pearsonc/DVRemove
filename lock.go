package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked reports that another run holds the lock.
var ErrLocked = errors.New("another dvremove run is already running")

// RunLock is the advisory lock that keeps two runs from overlapping (H12). It is a flock on a
// file in the state folder, which must be on a local disk: the kernel drops the lock when the
// holder dies, even by kill -KILL, so a killed run leaves nothing that blocks the next one, and
// the file's presence means nothing.
type RunLock struct{ file *os.File }

// AcquireLock takes the lock at path without waiting. It fails with ErrLocked, wrapped with the
// path, when another process holds it.
func AcquireLock(path string) (*RunLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: lock file %s is held", ErrLocked, path)
		}
		return nil, fmt.Errorf("failed to lock %s: %w", path, err)
	}
	return &RunLock{file: f}, nil
}

// Release gives the lock up. A run that exits releases it anyway.
func (l *RunLock) Release() { l.file.Close() }
