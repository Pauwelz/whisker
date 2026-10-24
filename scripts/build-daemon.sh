#!/bin/sh
# Build the Daemon into bin/whisker-daemon, with the version from manifest.json.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
version=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$root/manifest.json")
[ -n "$version" ] || { echo "no version in manifest.json" >&2; exit 1; }
cd "$root/daemon"
CGO_ENABLED=0 go build -ldflags "-X main.version=$version" -o "$root/bin/whisker-daemon" ./cmd/whisker-daemon
