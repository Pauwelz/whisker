package contract

import (
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
