#!/bin/sh
# Download the Daemon for the version in manifest.json, check it against SHA256SUMS, move it to bin/.
# Exit codes (also in docs/releasing.md and state/Daemon.js):
#   0 OK, 20 no version in manifest.json, 21 no SHA256SUMS line, 22 download failed, 23 checksum mismatch.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
asset=whisker-daemon-linux-amd64
# WHISKER_RELEASE_URL exists so the tests can use a file:// server. Only then does curl allow file://.
base=${WHISKER_RELEASE_URL:-https://github.com/Pauwelz/whisker/releases/download}
proto='=https'
[ -z "${WHISKER_RELEASE_URL:-}" ] || proto='=https,file'
. "$root/scripts/manifest-version.sh"
version=$(manifest_version "$root")
[ -n "$version" ] || exit 20
expected=$(sed -n "s/^\([0-9a-f]\{64\}\)  $asset\$/\1/p" "$root/SHA256SUMS" 2>/dev/null || true)
[ -n "$expected" ] || exit 21
mkdir -p "$root/bin"
tmp=$(mktemp "$root/bin/.whisker-daemon.XXXXXX")
trap 'rm -f "$tmp"' EXIT
# -q first: ignore ~/.curlrc.
curl -q --proto "$proto" --proto-redir =https --max-filesize 100M --fail --silent --show-error --location --max-time 300 --output "$tmp" "$base/v$version/$asset" || exit 22
actual=$(sha256sum "$tmp" | cut -d' ' -f1)
[ "$actual" = "$expected" ] || exit 23
chmod 755 "$tmp"
mv "$tmp" "$root/bin/whisker-daemon"
