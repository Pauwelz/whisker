package main

import (
	"os"
	"syscall"
)

// privateStdout makes the frame encoder the only writer of the stream the Plugin reads.
// It returns a private copy of fd 1, then points fd 1 and os.Stdout at stderr.
// A library that prints to stdout then writes to the log, not into the frame stream.
func privateStdout() (*os.File, error) {
	fd, err := syscall.Dup(syscall.Stdout)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	if err := syscall.Dup3(syscall.Stderr, syscall.Stdout, 0); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	os.Stdout = os.Stderr
	return os.NewFile(uintptr(fd), "frames"), nil
}
