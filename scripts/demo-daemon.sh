#!/bin/sh
# Build the demo Daemon (made-up Chats, for screenshots) into bin/whisker-daemon.
# Restart the shell to load it. Run scripts/build-daemon.sh and restart again to go back.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/scripts/manifest-version.sh"
version=$(manifest_version "$root")
[ -n "$version" ] || { echo "no version in manifest.json" >&2; exit 1; }
cd "$root/daemon"
CGO_ENABLED=0 go build -ldflags "-X main.version=$version" -o "$root/bin/whisker-daemon" ./cmd/whisker-demo
