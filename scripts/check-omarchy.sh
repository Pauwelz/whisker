#!/bin/sh
# Compile Panel.qml and Service.qml against the shell of an Omarchy release, to catch APIs that release lacks.
# Usage: scripts/check-omarchy.sh [tag]   (default v4.0.0, the oldest supported release)
# Needs curl, quickshell (qs) and a Wayland session. Exits 0 when both files compile.
# It catches missing types, imports and properties. A call to a missing function fails only at run time, so it does not.
set -eu
tag=${1:-v4.0.0}
root=$(cd "$(dirname "$0")/.." && pwd)
cache=${XDG_CACHE_HOME:-$HOME/.cache}/whisker/omarchy-$tag
files="Panel.qml Service.qml"

if [ ! -d "$cache/shell" ]; then
  mkdir -p "$cache"
  curl -sfL "https://codeload.github.com/basecamp/omarchy/tar.gz/refs/tags/$tag" \
    | tar xz -C "$cache" --strip-components=1 --wildcards '*/shell/*'
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -r "$cache/shell" "$work/shell"
cat > "$work/shell/shell.qml" <<EOF
import QtQuick
import Quickshell
ShellRoot {
  Component.onCompleted: {
    for (const name of "$files".split(" ")) {
      const c = Qt.createComponent("file://$root/" + name)
      console.log("WHISKER-CHECK", name, c.status === Component.Ready ? "OK" : "FAIL " + c.errorString())
    }
  }
}
EOF

# quickshell ignores Qt.quit(), so wait for both result lines and stop it.
qs -p "$work/shell" > "$work/log" 2>&1 &
pid=$!
i=0
while [ "$(grep -ac WHISKER-CHECK "$work/log")" -lt "$(echo $files | wc -w)" ] && [ $i -lt 60 ] && kill -0 $pid 2>/dev/null; do
  sleep 0.5; i=$((i + 1))
done
kill $pid 2>/dev/null || true
out=$(grep -a "WHISKER-CHECK" "$work/log" | sed 's/.*WHISKER-CHECK //') || true
[ -n "$out" ] || { echo "quickshell printed no result" >&2; exit 2; }
echo "Omarchy $tag:"
echo "$out"
if echo "$out" | grep -q FAIL; then exit 1; fi
