package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "XDG_DATA_HOME="+runtimeDir, "XDG_CONFIG_HOME="+runtimeDir,
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

// SIGTERM, SIGINT and SIGHUP end the Daemon like stdin EOF, so Run wipes the plain copies of Media.
func TestSignalStopsTheDaemonCleanly(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		bin, dir := buildDaemon(t), t.TempDir()
		c := start(t, bin, dir)
		if !c.out.Scan() {
			t.Fatal("Daemon sent no frame")
		}
		if err := c.cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		if got := c.exitCode(); got != contract.ExitOK {
			t.Errorf("exit code after %v = %d, want %d", sig, got, contract.ExitOK)
		}
	}
}

// The Plugin applies each stdout line as a frame, so every line must be a JSON-RPC 2.0 frame.
func TestEveryStdoutLineIsAJSONRPCFrame(t *testing.T) {
	c := start(t, buildDaemon(t), t.TempDir())
	io.WriteString(c.stdin.(io.Writer), `{"jsonrpc":"2.0","id":1,"method":"nope"}`+"\n")
	c.stdin.Close()
	lines := 0
	for c.out.Scan() {
		lines++
		var f struct {
			JSONRPC string `json:"jsonrpc"`
		}
		if err := json.Unmarshal(c.out.Bytes(), &f); err != nil || f.JSONRPC != "2.0" {
			t.Errorf("stdout line %q is not a JSON-RPC 2.0 frame", c.out.Text())
		}
	}
	if lines == 0 {
		t.Error("Daemon sent no frame")
	}
	c.exitCode()
}

// plant writes a file under $XDG_RUNTIME_DIR/whisker and returns its path.
func plant(t *testing.T, dir, rel string) string {
	t.Helper()
	p := filepath.Join(dir, "whisker", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// The start wipe needs no key: the keyring is unavailable here, and the Profile never opens.
// A pasted image of a Draft stays.
func TestStartWipesPlainMediaButKeepsPastedImages(t *testing.T) {
	bin, dir := buildDaemon(t), t.TempDir()
	stale := plant(t, dir, "media/chat/id.jpg")
	pasted := plant(t, dir, "pasted/a/Pasted image.png")
	c := start(t, bin, dir)
	if !c.out.Scan() { // the keyring frame: the wipe ran before Run
		t.Fatal("Daemon sent no frame")
	}
	if exists(stale) {
		t.Error("the plain copy stayed after start")
	}
	c.stdin.Close()
	c.exitCode()
	if !exists(pasted) {
		t.Error("the pasted image did not survive")
	}
}

// A closed stdout reader must not kill the Daemon with SIGPIPE (exit 141), and the exit wipe must run.
func TestBrokenStdoutExitsWithoutSigpipe(t *testing.T) {
	bin, dir := buildDaemon(t), t.TempDir()
	stale := plant(t, dir, "media/chat/id.jpg")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+dir, "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus")
	cmd.Stdout = w
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	r.Close() // no reader is left: the next frame write fails
	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"nope"}`+"\n")
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 141 {
		t.Fatal("the Daemon died of SIGPIPE")
	}
	if exists(stale) {
		t.Error("the plain copy stayed after exit")
	}
}
