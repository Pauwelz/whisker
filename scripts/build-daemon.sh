#!/bin/sh
# Build the Daemon into bin/whisker-daemon, with the version from manifest.json.
# --dev adds the dev build tag: the Daemon then logs the type of each Unsupported message to stderr. Releases never use it.
set -eu
tags=
if [ "${1:-}" = "--dev" ]; then
  tags=dev
elif [ $# -gt 0 ]; then
  echo "usage: $0 [--dev]" >&2
  exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/scripts/manifest-version.sh"
version=$(manifest_version "$root")
[ -n "$version" ] || { echo "no version in manifest.json" >&2; exit 1; }
cd "$root/daemon"
CGO_ENABLED=0 go build -trimpath -tags "$tags" -ldflags "-X main.version=$version" -o "$root/bin/whisker-daemon" ./cmd/whisker-daemon
