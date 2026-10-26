// Command whisker-daemon is the whisker Daemon. It speaks JSON-RPC over stdio (see docs/contract.md).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/profile"
	"github.com/Pauwelz/whisker/daemon/internal/profilelock"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// version is set at build time from manifest.json (see scripts/build-daemon.sh).
var version = "dev"

// lockWait covers a shell restart, where the old Daemon is still stopping.
const lockWait = 5 * time.Second

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	// Before any library code writes to fd 1: only the frame encoder may write there.
	frames, err := privateStdout()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot protect stdout:", err)
		os.Exit(contract.ExitError)
	}
	os.Exit(run(frames))
}

func run(frames io.Writer) int {
	// No ptrace, no /proc/<pid>/fd and no core dump for other programs of this user: the stdin pipe stays closed to them.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		fmt.Fprintln(os.Stderr, "cannot protect the process:", err)
		return contract.ExitError
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		fmt.Fprintln(os.Stderr, "XDG_RUNTIME_DIR is not set")
		return contract.ExitError
	}
	runtimeDir = filepath.Clean(runtimeDir)
	if err := profilelock.CheckRuntimeDir(runtimeDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return contract.ExitError
	}
	lock, err := profilelock.Acquire(filepath.Join(runtimeDir, "whisker"), lockWait)
	if errors.Is(err, profilelock.ErrInUse) {
		fmt.Fprintln(os.Stderr, "profile in use")
		return contract.ExitProfileInUse
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile lock failed:", err)
		return contract.ExitError
	}
	defer lock.Release()
	dataDir, err := xdgDir("XDG_DATA_HOME", ".local/share")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return contract.ExitError
	}
	// A wipe of the Profile or of the plain Media must never reach the other root.
	if within(dataDir, runtimeDir) || within(runtimeDir, dataDir) {
		fmt.Fprintln(os.Stderr, "XDG_DATA_HOME and XDG_RUNTIME_DIR overlap")
		return contract.ExitError
	}
	contract.KeyringPlain = func() bool { return keystore.PlainKeyring(dataDir) }
	configDir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return contract.ExitError
	}
	prefs := settings.New(filepath.Join(configDir, "whisker", "settings.json"))
	p := profile.New(filepath.Join(dataDir, "whisker"), keystore.New(keystore.SecretService{}, clock.Real))
	defer p.Close()
	// The lock is held, so no live Daemon owns these copies. No key is needed, so a locked keyring does not skip this.
	p.Media().Wipe()
	whatsapp.SetDeviceIdentity(version)
	newPort := func() (whatsapp.Port, error) { return whatsapp.NewClient(p.Device()) }
	return contract.Run(stdinUntilSignal(), frames, os.Stderr, p, prefs, newPort, desktop.DBus{LogOut: os.Stderr})
}

// xdgDir returns the absolute path in the variable. The XDG spec says to treat a relative path as unset, so it uses ~/<fallback>.
func xdgDir(name, fallback string) (string, error) {
	if v := os.Getenv(name); filepath.IsAbs(v) {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", errors.New("no home directory")
	}
	return filepath.Join(home, fallback), nil
}

// within tells whether path is dir or lies inside dir. Both paths are clean and absolute.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// stdinUntilSignal reads stdin, and ends like EOF on SIGTERM, SIGINT or SIGHUP.
// It also ignores SIGPIPE, so a broken stderr gives a write error instead of SIGPIPE.
// Run then returns normally, so it wipes the plain copies of Media.
func stdinUntilSignal() io.Reader {
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.Copy(pw, os.Stdin)
		pw.Close()
	}()
	sig := make(chan os.Signal, 1)
	signal.Ignore(syscall.SIGPIPE)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-sig
		pw.Close()
	}()
	return pr
}
