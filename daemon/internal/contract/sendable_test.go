package contract

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStatSendableNeverBlocksOnAFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := statSendable(fifo); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO must not be sendable")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("statSendable blocked on a FIFO")
	}
}

func TestOpenSendableRefusesAFIFOAndALinkToDevZeroWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	zero := filepath.Join(dir, "zero")
	if err := os.Symlink("/dev/zero", zero); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, zero} {
		done := make(chan error, 1)
		go func() {
			f, _, err := openSendable(path)
			if f != nil {
				f.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s must not be sendable", path)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("openSendable blocked on %s", path)
		}
	}
}
