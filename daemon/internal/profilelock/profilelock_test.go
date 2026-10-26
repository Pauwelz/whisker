package profilelock_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/profilelock"
)

func TestCheckRuntimeDir(t *testing.T) {
	good := t.TempDir()
	if err := os.Chmod(good, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := profilelock.CheckRuntimeDir(good); err != nil {
		t.Errorf("private directory refused: %v", err)
	}
	open := t.TempDir()
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(good, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(good, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"symlink": link, "relative": "run/user", "missing": filepath.Join(good, "none"), "open mode": open, "file": file} {
		if err := profilelock.CheckRuntimeDir(dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

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
