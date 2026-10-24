// Command whisker-daemon is the whisker Daemon. It speaks JSON-RPC over stdio (see docs/contract.md).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/profilelock"
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
	return contract.Run(os.Stdin, os.Stdout, os.Stderr)
}
