// Command whisker-daemon is the whisker Daemon. It speaks JSON-RPC over stdio (see docs/contract.md).
package main

import (
	"fmt"
	"os"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
)

// version is set at build time from manifest.json (see scripts/build-daemon.sh).
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	os.Exit(contract.Run(os.Stdin, os.Stdout, os.Stderr))
}
