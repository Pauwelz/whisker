# Sourced by the other scripts. manifest_version ROOT prints the version in ROOT/manifest.json, or nothing.
manifest_version() {
  sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$1/manifest.json"
}
