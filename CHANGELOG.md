# Changelog

All notable changes to whisker are in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and whisker uses [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The `chatMerged` notification tells the Plugin when two Chats merge.
- The Attach button stages a file. The file waits in the Draft as "Attached: name" with a remove button (✕). Enter sends it with the text as caption. The Draft keeps the file over a Chat switch, closing the pop-out and a Daemon restart. Starting an Edit drops the file. `draft.set` takes an `attachment` param for the file.

### Fixed

- The bar count now matches the Chat list: a Chat with no activity no longer counts. The count also updates when a Mute ends.
- History Sync now fills Pin, Mute, unread and the Disappearing timer of a Chat that a live Message created first. A later History Sync batch no longer overwrites a value that a live event set.
- A Chat merge no longer deletes Media files when a move fails. The Daemon logs "media move failed" and keeps the files.
- A Message that you sent while two Chats merged now shows as sent in the merged Chat. Before, it stayed Sending.
- A merge of two Chats keeps the text of both Drafts. Before, the Draft of the LID Chat was lost. The merge also runs when the LID shows only as a Draft or as a group sender.
- A Quote and a staged file no longer combine, like an Edit and a file. Staging a file drops the Quote. Starting a Quote drops the file. Before, the Quote dropped without notice when the file went out.
- The Draft text always saves. A file that is gone, or that is not a regular file, no longer blocks the save of the text. The Draft drops such a file when you open the Chat.
- A contact no longer shows as two Chats, one with the contact name and one with the phone number. WhatsApp sends the LID to phone number mapping after the Chat, and the Daemon now merges the two Chats, with their Messages, Drafts and Media.
- Incoming Messages show again. The Daemon took every ordinary Message for a revoke and dropped it.
- A Message that WhatsApp addresses by LID goes to the Chat of the phone JID, not to a second Chat.
- The Attach button opens the file picker. The picker did not load.
- A location has an Open map button that opens the map in the browser. The link in the Message text did not open, and `xdg-open` refused the command.
- History Sync no longer adds a second Chat for a contact that WhatsApp sends by LID. The Daemon uses the phone JID that the conversation carries.
- The linked device shows as "whisker" (a desktop) on the phone, not as "Other device". The name applies after a new link. Users linked with rc.2 keep "Other device" until they link again.
- "Load more" no longer shows in the settings view.

## [1.0.0-rc.2] - 2026-10-24

### Added

- Each Chat keeps its Draft: the text and a pending Quote or Edit target. A Chat switch, closing the pop-out and a Daemon restart keep it. Sending clears it.
- A Disappearing Message and its Media leave the Profile when the timer of the Chat ends, also when whisker was off. A Message you send carries the timer of its Chat.
- A voice note has a Play and Stop button in the pop-out. It plays with `mpv --no-video`. Closing the pop-out stops it. The sender sees the note as played, unless Read Receipts is Off. In On interaction mode, playing also sends Read for that note only.
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
- Images, stickers and voice notes download by themselves. Video, audio and documents download on a click. The Daemon keeps received Media encrypted in the Profile and rejects a file that fails its integrity check.
- The pop-out shows images and stickers. Video opens in `mpv`, other Media in its default app. "Save as" writes a plain copy where you choose.
- When the phone removes the Companion Device, the Daemon deletes the Profile and the pop-out goes back to linking.
- The Daemon knows when the pop-out is open and which Chat it shows. Opening a Chat clears its unread mark, also on the phone. A new Message marks its Chat unread.
- An Attach button in the composer picks an image or a document and sends it. The composer text is the caption. Sent Media is stored encrypted and opens again after a restart.
- Read Receipts follow the setting: Off sends none, On open sends them for the Chat you view, On interaction sends them for the whole Chat after you send a Message there.
- Messages show Reactions as emoji with a count. React offers the six default emoji; picking your own again removes it. A Participant has one Reaction per Message. Reactions from the phone and from others show the same way.
- A reply can quote a Message. The pop-out shows who wrote the quoted Message and a preview. Quotes from the phone and from others show the same way.
- With Read Receipts on "On interaction", a Reaction or a Quote reply sends the Read Receipts of the Chat, like a sent Message.
- A new Message raises a desktop notification, except in a Muted Chat, in the Attended Chat, or when notifications are off. A click on it opens the pop-out on that Chat.
- The bar shows the number of unread Chats, without Muted Chats. It shows only the icon when no Chat is unread.
- A Direct Chat header shows Online or Last Seen. A Chat shows "typing…" or "recording…", with the Participant name in a Group Chat.
- Polls, events and view-once Media show a one-line text that says to open them on the phone. A contact card shows the name and number. A location shows its name and a map link that opens in the browser. No Message is blank.
- A mention shows the Participant name in bold. A Message that mentions you notifies even in a Muted Group Chat.
- whisker sends Composing while you type, if "send typing" is on. It sends Online while the pop-out is open, if "send online" is on.
- The pop-out has fixed keyboard shortcuts: Escape goes back one level, and Ctrl+F focuses the search field. The README lists them.

### Fixed

- A Profile whose store key is in the keyring but wrong no longer stops the Daemon. The Daemon deletes the unreadable Profile and starts linking.
- Log out deletes the Profile and its key also when WhatsApp is out of reach. The phone then still lists the device until you remove it there.
- The Daemon deletes the plain copies of Media also on SIGTERM and SIGINT. Opening a Chat from a notification click releases the Media of the Chat you left, stops its voice note and stops Composing.
- A late History Sync batch no longer undoes pin, mute or unread marks that the phone changed after the sync began.
- The bar shows the unread count of all Chats, not only of the Chats that the pop-out has loaded. A search does not change it.
- Edit ends 15 minutes after sending, the limit of WhatsApp. It was 20 minutes.

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
