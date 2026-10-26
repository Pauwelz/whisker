#!/bin/sh
# Print the CHANGELOG.md section of version V, without its heading. Exit 1 when the section has no text.
set -eu
[ $# -eq 1 ] || { echo "usage: release-notes.sh V" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
notes=$(awk -v h="## [$1]" 'f && /^## \[/ {exit} f; $0 == h || index($0, h " ") == 1 {f = 1}' "$root/CHANGELOG.md")
[ -n "$(printf '%s' "$notes" | tr -d '[:space:]')" ] || exit 1
printf '%s\n' "$notes" | sed '/./,$!d'
