// Package profilelock stops a second Daemon from using a Profile that another Daemon owns.
package profilelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrInUse means another process holds the Profile lock.
var ErrInUse = errors.New("profile in use")

const pollInterval = 50 * time.Millisecond

// Lock is a held Profile lock.
type Lock struct{ file *os.File }

// Acquire takes the lock in dir and waits up to wait for a holder to release it.
// The kernel drops a flock when its process dies, so a crash leaves no stale lock.
func Acquire(dir string, wait time.Duration) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "profile.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{file: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("lock: %w", err)
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrInUse
		}
		time.Sleep(pollInterval)
	}
}

// Release drops the lock.
func (l *Lock) Release() { l.file.Close() }
