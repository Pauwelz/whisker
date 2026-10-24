package profilelock_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/profilelock"
)

func TestSecondAcquireFailsWithInUse(t *testing.T) {
	dir := t.TempDir()
	first, err := profilelock.Acquire(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if _, err := profilelock.Acquire(dir, 100*time.Millisecond); !errors.Is(err, profilelock.ErrInUse) {
		t.Errorf("err = %v, want ErrInUse", err)
	}
}

func TestAcquireWaitsForARelease(t *testing.T) {
	dir := t.TempDir()
	first, err := profilelock.Acquire(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(200*time.Millisecond, func() { first.Release() })
	second, err := profilelock.Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatalf("err = %v, want the lock after the release", err)
	}
	second.Release()
}
