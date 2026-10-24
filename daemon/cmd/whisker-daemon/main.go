// Command whisker-daemon is the whisker Daemon. It speaks JSON-RPC over stdio (see docs/contract.md).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/contract"
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
	os.Exit(run())
}

func run() int {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		fmt.Fprintln(os.Stderr, "XDG_RUNTIME_DIR is not set")
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
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "no home directory")
			return contract.ExitError
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "no home directory")
			return contract.ExitError
		}
		configDir = filepath.Join(home, ".config")
	}
	prefs := settings.New(filepath.Join(configDir, "whisker", "settings.json"))
	p := profile.New(filepath.Join(dataDir, "whisker"), keystore.New(keystore.SecretService{}, clock.Real))
	defer p.Close()
	whatsapp.SetDeviceIdentity(version)
	newPort := func() (whatsapp.Port, error) { return whatsapp.NewClient(p.Device()) }
	return contract.Run(stdinUntilSignal(), os.Stdout, os.Stderr, p, prefs, newPort)
}

// stdinUntilSignal reads stdin, and ends like EOF on SIGTERM or SIGINT.
// Run then returns normally, so it wipes the plain copies of Media.
func stdinUntilSignal() io.Reader {
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.Copy(pw, os.Stdin)
		pw.Close()
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		pw.Close()
	}()
	return pr
}
