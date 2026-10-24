# Changelog

All notable changes to whisker are in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and whisker uses [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0-rc.1] - 2026-10-24

### Added

- A bar icon with a pop-out that shows when the Daemon is ready.
- The Daemon speaks JSON-RPC over stdio and answers `--version`.
- `scripts/build-daemon.sh` builds the Daemon for local development.
- A second Daemon on one Profile waits 5 s, then exits with code 11. The pop-out shows a "Profile in use" screen.
- `CONTRIBUTING.md` explains how to send a pull request to `dev`.
- `scripts/fetch-daemon.sh` downloads the Daemon for the Plugin version and checks it against `SHA256SUMS`.
- The Plugin downloads a missing or wrong-version Daemon. A failed download shows "Daemon download failed" with Retry.
- `docs/releasing.md` lists the release steps.
- The Daemon keeps its device store and chat store in encrypted SQLite. The store key lives in the Secret Service.
- The pop-out shows "keyring unavailable" with Retry when the keyring does not answer in 30 s.
- The Daemon deletes a Profile whose store key is lost, and starts a fresh one.
