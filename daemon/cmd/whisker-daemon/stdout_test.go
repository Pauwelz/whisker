package main

import (
	"io"
	"os"
	"syscall"
	"testing"
)

// redirectFd points fd at a new pipe and returns the read end. The cleanup restores fd.
func redirectFd(t *testing.T, fd int) *os.File {
	t.Helper()
	saved, err := syscall.Dup(fd)
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Dup3(int(w.Fd()), fd, 0); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() {
		syscall.Dup3(saved, fd, 0)
		syscall.Close(saved)
		r.Close()
	})
	return r
}

func TestPrivateStdoutKeepsStrayWritesOutOfTheFrameStream(t *testing.T) {
	savedStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = savedStdout })
	frameStream := redirectFd(t, syscall.Stdout)
	logStream := redirectFd(t, syscall.Stderr)

	frames, err := privateStdout()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := syscall.Write(syscall.Stdout, []byte("stray")); err != nil { // a library that writes to fd 1
		t.Fatal(err)
	}
	os.Stdout.WriteString("!")
	frames.WriteString("frame")
	frames.Close()

	// Close fd 1 and fd 2 to end both pipes. The cleanup restores them.
	syscall.Close(syscall.Stdout)
	syscall.Close(syscall.Stderr)
	if got, _ := io.ReadAll(frameStream); string(got) != "frame" {
		t.Errorf("frame stream = %q, want only %q", got, "frame")
	}
	if got, _ := io.ReadAll(logStream); string(got) != "stray!" {
		t.Errorf("log stream = %q, want %q", got, "stray!")
	}
}
