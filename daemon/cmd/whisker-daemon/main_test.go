package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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

// runtimeDir makes a private directory, as systemd does for XDG_RUNTIME_DIR. TempDir makes it 0755.
func runtimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
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
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "XDG_DATA_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(),
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
	bin, dir := buildDaemon(t), runtimeDir(t)
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
	bin, dir := buildDaemon(t), runtimeDir(t)
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
		bin, dir := buildDaemon(t), runtimeDir(t)
		c := start(t, bin, dir)
		if !c.out.Scan() {
			t.Fatal("Daemon sent no frame")
		}
		stale := plant(t, dir, "media/chat/id.jpg") // after the start wipe
		if err := c.cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		if got := c.exitCode(); got != contract.ExitOK {
			t.Errorf("exit code after %v = %d, want %d", sig, got, contract.ExitOK)
		}
		if exists(stale) {
			t.Errorf("the plain copy stayed after %v", sig)
		}
	}
}

// The Plugin applies each stdout line as a frame, so every line must be a JSON-RPC 2.0 frame.
func TestEveryStdoutLineIsAJSONRPCFrame(t *testing.T) {
	c := start(t, buildDaemon(t), runtimeDir(t))
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
	bin, dir := buildDaemon(t), runtimeDir(t)
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

// Go raises SIGPIPE for a failed write to fd 1 or fd 2. The frames use a copy of fd 1, so only a broken stderr (the log) matters.
// Without signal.Ignore the first log line kills the Daemon, and the exit wipe never runs.
func TestBrokenStderrDoesNotKillTheDaemon(t *testing.T) {
	bin, dir := buildDaemon(t), runtimeDir(t)
	stale := plant(t, dir, "media/chat/id.jpg")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close() // no reader is left: the first log line fails
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+dir, "XDG_DATA_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(),
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus")
	cmd.Stderr = w
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
	w.Close()
	bufio.NewScanner(out).Scan() // the keyring frame, sent after the log line
	in.Close()
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.Sys().(syscall.WaitStatus).Signaled() {
		t.Fatalf("the Daemon died of %v", exitErr.Sys().(syscall.WaitStatus).Signal())
	}
	if exists(stale) {
		t.Error("the plain copy stayed after exit")
	}
}

// Another program of the same user must not open the stdin pipe through /proc/<pid>/fd.
func TestProcessIsNotDumpable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can open the fd anyway")
	}
	c := start(t, buildDaemon(t), runtimeDir(t))
	if !c.out.Scan() {
		t.Fatal("Daemon sent no frame")
	}
	f, err := os.OpenFile(fmt.Sprintf("/proc/%d/fd/0", c.cmd.Process.Pid), os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		t.Error("the stdin pipe of the Daemon is open to the same user")
	}
	c.stdin.Close()
	c.exitCode()
}

// run exits with ExitError, before any lock or wipe, for each unsafe folder setup.
func TestUnsafeFolderSetupExitsWithError(t *testing.T) {
	bin := buildDaemon(t)
	open := t.TempDir()
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	runtime := runtimeDir(t)
	cases := map[string][]string{
		"relative runtime dir": {"XDG_RUNTIME_DIR=relative/run", "XDG_DATA_HOME=" + t.TempDir()},
		"open runtime dir":     {"XDG_RUNTIME_DIR=" + open, "XDG_DATA_HOME=" + t.TempDir()},
		"data in runtime":      {"XDG_RUNTIME_DIR=" + runtime, "XDG_DATA_HOME=" + filepath.Join(runtime, "data")},
		"runtime in data":      {"XDG_RUNTIME_DIR=" + runtime, "XDG_DATA_HOME=" + filepath.Dir(runtime)},
	}
	for name, env := range cases {
		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(), append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus")...)
		in, _ := cmd.StdinPipe()
		defer in.Close()
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != contract.ExitError {
			t.Errorf("%s: err = %v, want exit code %d", name, err, contract.ExitError)
		}
	}
}

func TestXDGDirIgnoresARelativePath(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	t.Setenv("XDG_DATA_HOME", "relative/data")
	if got, err := xdgDir("XDG_DATA_HOME", ".local/share"); err != nil || got != "/home/someone/.local/share" {
		t.Errorf("relative path: got %q, %v", got, err)
	}
	t.Setenv("XDG_DATA_HOME", "/data/x/")
	if got, _ := xdgDir("XDG_DATA_HOME", ".local/share"); got != "/data/x" {
		t.Errorf("absolute path: got %q", got)
	}
}

func TestWithin(t *testing.T) {
	for path, want := range map[string]bool{"/a/b": true, "/a": true, "/a/b/c": true, "/ab": false, "/": false} {
		if got := within(path, "/a"); got != want {
			t.Errorf("within(%q, /a) = %v", path, got)
		}
	}
}
