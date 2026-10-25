# Changelog

All notable changes to whisker are in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and whisker uses [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The QR code of the linking screen is larger, centred, and shows the whisker logo in the middle. "pair code" and "qr code" in the header switch between the two ways to link. The Pair Code form is a prompt line like the composer: type the phone number and press Enter. The Pair Code then shows large, with the steps on the phone above it. The Daemon now makes it at error correction level Q, so the code stays readable under the logo.
- The Chat header shows the round Profile picture of a contact, or the group icon, in place of the Chat sign. Without a picture, or when WhatsApp hides it, the sign stays. The Daemon keeps the small picture in the encrypted chat store for 24 hours. It has the new method `chat.picture` and the new notification `pictureChanged`.
- The Chat links offer "mark unread" or "mark read". It changes only the Read Marker and sends no Read Receipt. Marking the open Chat unread returns to the Chat list, so the mark stays. The Daemon has the new method `chat.markUnread`.
- Hover toolbar on a Message: React, Reply and More take the place of the time and Delivery State. React shows the quick emoji with your current Reaction outlined; a click elsewhere closes it. Right-click and More open a menu with the actions that the Message allows, including Copy text. Reactions show as chips with a count below the Message; your own is outlined, a click adds or removes it, and hover lists who reacted. The composer shows the Quote or Edit target with a ✕ that cancels it. Delete for me removes the Message on your devices only; the Daemon has the new method `chat.deleteForMe`.
- "+ new chat" in the pop-out header opens the new Chat view. A search field lists Phone Contacts ("open" or "new"), then Participants known only by a Push Name, and a note that new Group Chats are created on the phone. A pick opens the Chat. An empty Chat says "No messages yet. Your first message starts the chat." The Daemon has the new methods `contacts.search` and `chat.start`, and the error -32010 (invalid chat).
- Each Message is one line in the pop-out: time, Delivery State, sender, then the text. The next Messages of one sender hide the time and sender but keep the Delivery State. Group Chat senders have their own colour. A sender known only by a Push Name shows as "~ name +number". A Message that mentions you is highlighted. Day separators and system lines are quiet. A click on a Quote jumps to the quoted Message. An edited Message ends with "(edited)". A failed Message shows a red "!" and "not sent · retry" below it. A click on that line sends it again.
- The Daemon adds `senderPush` (the sender has only a Push Name) and `mentionsMe` (the Message mentions you) to a Message. Messages from before the update do not show as mentioning you.
- The Chat list is one dense line per Chat in IRC style. A sign shows a Direct Chat, a Group Chat or the Self Chat. Pinned Chats have a mark, a Muted Chat has a mute sign and a grey unread count, and a Chat that mentions you has an "@". "is typing…" shows in the line. The filters all, unread and groups sit next to the search field. An empty list shows the faded logo and a short text. `chats.list` returns `unreadCount` and `mention` for this.
- The whisker logo. The bar shows a one-colour cat in the bar foreground colour, with the unread count. The pop-out header shows the full logo. The files are `assets/whisker.svg` and `assets/whisker-bar.svg`.
- The `chatMerged` notification tells the Plugin when two Chats merge.
- The Attach button stages a file. The file waits in the Draft as "Attached: name" with a remove button (✕). Enter sends it with the text as caption. The Draft keeps the file over a Chat switch, closing the pop-out and a Daemon restart. Starting an Edit drops the file. `draft.set` takes an `attachment` param for the file.
- Media shows as a compact chip with WhatsApp's thumbnail (blurred for a video that is not downloaded). An image shows only as the thumbnail of its chip, never at full size. Without WhatsApp's thumbnail, a downloaded image shows its own file small. Chips without a thumbnail show a file-type badge such as IMG, VID, GIF, PDF or MP3. Media whose thumbnail WhatsApp keeps apart, mostly from History Sync, gets it when its Chat opens. A GIF says "GIF" and opens in mpv on a loop; the Media object has the field `gif`. A click on the thumbnail or name runs the main action. The chip has download, open and "save as…" links, a progress bar, and a red failure with a retry link. A voice note has a play button and a dot until you play it; the dot stays away after a restart. Audio, voice and video chips show their length. A sticker shows as itself. The Media object has the fields `thumbnail`, `duration` and `played`.
- You can pin, unpin, mute and unmute a Chat, and the change syncs to your phone. Hover a Chat line for the Chat links pin, mark unread and mute. The ⋯ button in the Chat header shows the same links in the header. A click on mute shows the choices 8h, 1w and always in their place. The Chat header shows how long a Muted Chat stays muted. The Daemon has the new methods `chat.pin` and `chat.mute`, and `chats.list` returns `mutedUntil`. A 4th pin shows "You can pin up to 3 chats". An error now shows a short notice in the pop-out, chosen by error code.
- Type a full phone number in the new Chat view to message someone who is not a Phone Contact. A phone number row shows "checking WhatsApp…", then "on WhatsApp · message" (a click opens the Chat) or "not on WhatsApp". A number that is too short or not international, and a failed lookup, show a short message. The lookup waits 400 ms after the last key, so typing does not ask WhatsApp for every digit. The Daemon has the new method `contacts.lookup`.
- Archived Chats no longer show in the Chat list or count as unread. A new "archived" filter lists them. The search field still finds them. An unarchived Chat shows again. When "Keep chats archived" is off on your phone, a new Message unarchives its Chat and notifies. When it is on, the Chat stays archived and silent, unless the Message mentions you. The Daemon has the new `chats.list` param `archived`.

### Changed

- The "settings" button sits in the pop-out header, next to "+ new chat".
- The pop-out keeps one size in every view. The back button "←" sits at the right of each top bar, so the left side does not shift. The Chat sign or Profile picture sits in a slot with the size and place of the header logo. Chat names in the list, the header and the open Chat start at one position. The pin mark of a Chat follows its name.
- The header shows "● online" while contacts see you Online: whisker is connected and "send online" is on.
- With "send online" off, an open Chat no longer shows Online or Last Seen. WhatsApp may stop sending Presence then, so the line would go stale.
- The settings view has three sections: Privacy, Notifications and Account. Read Receipts is one row of three choices, with a line that says what the set mode does. Send online, Send typing and Notifications are on/off switches, each with a short description. Before, each setting was a separate button.
- The pop-out opens under the whisker icon in the bar, not in the middle of the bar.
- The pop-out follows the prototype (variant C). A run of Messages from one sender has one header line (sender and time) above its Messages, which use the full width; the Delivery State stays at the right end of each own Message, and the hover toolbar floats at its top right. The Chat list has one compact top bar with the unread count, a search row with flat filters, and no "whisker is linked." line. An open Chat has one top bar (back, sign, name, status, pin and mute marks, ⋯), a timeline of fixed height, a quiet "load older" link at its top, and a flat composer strip. The new Chat view has the same bar, search and contact rows.
- Error code -32007 now means "could not reach WhatsApp" (was "send failed"). New codes: -32010 invalid chat, -32011 pin limit and -32602 invalid params.

### Fixed

- After a link, Pinned Chats match the phone. The first App State sync after a link dropped its Pin events, so only the old pin of History Sync counted. Now a Pin from App State wins over the History Sync pin, also when it arrives first or when a LID Chat merges. Mutes and Read Markers of that first sync now also come through, in one refresh. Re-link to repair the pins of a device that is linked already.
- Older Messages of the user that come in History Sync keep their Delivery State. Before, they all showed one ✓ (sent), even when the other person had read them. A later History Sync moves a stored Message forward to its state. Re-link to repair Messages that are stored already.
- Older Messages from History Sync now show their Reactions. Before, the Daemon dropped the Reactions that History Sync carries, so only Reactions that arrived live showed.
- The composer is visible again: one bordered row with a "type a message" hint, an attach button and a send button. Before, an empty composer drew nothing.
- Messages use the width of the pop-out. The pop-out is wider and the sender column narrower, so a short Message no longer wraps over many lines.
- whisker now shows the names from the contacts on your phone. Before, it never received them after linking, so it showed Push Names ("~ name") or phone numbers.
- Older Messages in a Group Chat now show the name of the sender. Before, a sender that WhatsApp named by an internal ID (LID) showed as a long number, even when the phone contacts held a name.
- A Chat that you mute on your phone now stays muted for the right time. Before, the Daemon read the end time as seconds instead of milliseconds, so the Chat looked muted for ever.
- `chats.list` now lists a Chat that has a Draft, not only a Chat with a Message. A Chat that was started but has neither stays out of the list.
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
