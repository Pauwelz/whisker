# Changelog

All notable changes to whisker are in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and whisker uses [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A fresh Profile shows a QR code in the pop-out. The code refreshes while the user waits.
- A switch in the pop-out links with a Pair Code instead.
- The pop-out leaves the linking screen when the phone authorizes the Companion Device.
- Linking errors show in the pop-out with Retry.
- After linking, History Sync fills the chat store. The pop-out shows the Chat list: Pinned Chats first, then newest first, with marks for Muted and unread Chats.
- The Chat list shows the phone contact name, then the Push Name, then the phone number. The pop-out loads more Chats on request.
- The pop-out has a settings view: Read Receipts mode (Off, On open, On interaction), send online, send typing and notifications. The Daemon keeps them in `~/.config/whisker/settings.json`.
- Log out, after a confirmation, removes the Companion Device from the phone, deletes the Profile and returns to linking. The settings stay.
- A search field above the Chat list filters the Chats by Chat name and by the name of a Participant who wrote in them.
- A Chat opens from the Chat list. It shows paged history, newest at the bottom, with "Load older". A Group Chat shows the sender name on each Message.
- Text goes both ways. Enter sends and Shift+Enter adds a line. Your own Messages show their Delivery State: Sending, Sent, Delivered or Read.
- A Message sent while offline stays Pending and goes out once on reconnect. A Failed Message has Retry, and a retry never shows a Message twice.
- The pop-out shows "Connecting…" or "Offline", and the bar icon dims while offline. The Daemon connects again after a failed connect.
- Your sent Messages have Edit (for 20 minutes) and Delete. An edited Message shows "Edited". A deleted Message becomes a Tombstone at the same place. Edits and deletes from the phone or from other Participants show the same way.
- When the phone removes the Companion Device, the Daemon deletes the Profile and the pop-out goes back to linking.

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
