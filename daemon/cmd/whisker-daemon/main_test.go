package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
)

// child is a running whisker-daemon process.
type child struct {
	cmd   *exec.Cmd
	stdin io.Closer
	out   *bufio.Scanner
}

func buildDaemon(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "whisker-daemon")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

func start(t *testing.T, bin, runtimeDir string) *child {
	t.Helper()
	cmd := exec.Command(bin)
	// No session bus: the keyring fails at once, so these tests never touch the real keyring.
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "XDG_DATA_HOME="+runtimeDir,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return &child{cmd: cmd, stdin: in, out: bufio.NewScanner(out)}
}

func (c *child) exitCode() int {
	var exitErr *exec.ExitError
	if err := c.cmd.Wait(); errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

func TestSecondDaemonOnOneProfileExitsProfileInUse(t *testing.T) {
	bin, dir := buildDaemon(t), t.TempDir()
	first := start(t, bin, dir)
	if !first.out.Scan() { // the keyring frame: the first Daemon holds the lock
		t.Fatal("first Daemon sent no frame")
	}
	second := start(t, bin, dir)
	if got := second.exitCode(); got != contract.ExitProfileInUse {
		t.Errorf("second exit code = %d, want %d", got, contract.ExitProfileInUse)
	}
	first.stdin.Close()
	if got := first.exitCode(); got != contract.ExitOK {
		t.Errorf("first exit code = %d, want %d", got, contract.ExitOK)
	}
}

func TestRestartRaceDoesNotShowProfileInUse(t *testing.T) {
	bin, dir := buildDaemon(t), t.TempDir()
	old := start(t, bin, dir)
	if !old.out.Scan() {
		t.Fatal("old Daemon sent no frame")
	}
	old.stdin.Close() // the shell restarts: the old Daemon is still stopping
	next := start(t, bin, dir)
	if !next.out.Scan() {
		t.Fatal("new Daemon did not get the lock")
	}
	next.stdin.Close()
	if got := next.exitCode(); got != contract.ExitOK {
		t.Errorf("new exit code = %d, want %d", got, contract.ExitOK)
	}
	old.exitCode()
}
