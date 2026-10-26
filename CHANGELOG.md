# Changelog

All notable changes to whisker are in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and whisker uses [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A Show preview setting hides the message text in notifications.

### Changed

- The README and the architecture now say that Omarchy keeps the text of each notification in plain text for up to 24 h, also after the message is revoked, deleted or expires. Turn Show preview off to keep message text out of that history.
- The README now explains what whisker does, what it needs, how to link your phone and which dependencies it uses. It also states that whisker is unofficial and not affiliated with WhatsApp or Meta.
- The plugin description in Omarchy is longer and names whatsmeow. The bar widget description is "WhatsApp chats in a pop-out".

### Security

- A `#`, `?` or `%` in the data path no longer turns off store encryption. Before, the Daemon put the path unescaped in the SQLite URI, so such a path ended the file name early and dropped the encryption setting. Now the Daemon escapes the path.
- A click on a notification no longer puts the Chat id in a process argument. Before, the click command was `omarchy-shell pauwelz.whisker openChat <chat id>`, so other local users could read the phone number or group id in `/proc`. Omarchy also saved it in its notification history for up to 24 h. Now the command holds a random token, and the Daemon looks up the Chat. After a Daemon restart, an old notification opens the Chat list. The architecture now lists what whisker still passes in arguments: the path of a plain Media copy and the URL of a map link.
- Settings now shows a notice when the keyring that holds the store key has no password, because then a copy of your files holds the key. The README and the architecture notes now say what the store encryption protects in that case.
- whisker now shows when a contact's security code changes and when a new device joins your account. A change of a contact's key shows "Security code changed" in the Direct Chat, once for a repeat, without a notification or unread count. At each connection the Daemon reads your own device list. A device that was not in the earlier list shows "A new device was linked to your account" in your own Direct Chat. The first list after an update only fills the stored set. whatsmeow still accepts a changed key without asking and does not check signed device lists, so the Companion Device trusts the WhatsApp server. The architecture now says so.
- Media folders and files now have keyed hash names. The media folder no longer shows phone numbers or message ids. Folder and file modification times are the Unix epoch, and the plain copies in `$XDG_RUNTIME_DIR` use the same names. The first start renames existing files.
- A deleted Message no longer stays readable in the store files. The chat store and the device store now overwrite deleted rows, and the first start after this update rewrites both stores once to remove old leftovers. This covers a Message that expires, "Delete for me", an edit and a revoke.
- Decrypted photos, videos and documents no longer stay in `$XDG_RUNTIME_DIR/whisker/media/` after the Daemon stops. Before, a locked keyring skipped the wipe, and SIGHUP or a broken log stream (SIGPIPE) killed the Daemon before it wiped. Now the Daemon wipes the folder at every start, before it needs the key, and at every exit. SIGHUP stops it cleanly, and a broken log stream gives a write error instead. The Plugin also closes the Daemon stdin when it unloads. Pasted images of Drafts stay.
- A received location, live location, contact card, poll, event or unsupported message now disappears when the chat timer says so. Before, the Daemon read the Disappearing timer of text, image, video, audio, sticker and document messages only, so the other kinds stayed for ever, with their coordinates or phone number. The Daemon now reads the timer of every kind. When WhatsApp marks a live message as disappearing but sends no timer, it uses the timer of the chat. Messages that whisker stored before this update keep `expires_at = 0`: the Daemon cannot tell them from a message that was sent before the chat got its timer.
- An app that can write your attach folder can no longer swap an attached file for a link to a file you did not choose. Before, whisker followed a symlink at send time and sent the target under the old name. Now whisker stores the real path of an attached file, and refuses a path with a symlink when you send. A FIFO or `/dev/zero` can no longer hang the Daemon.
- A reply no longer sends the names of your Phone Contacts. When you quote a message that mentions someone, whisker now keeps the original text in the quote. Before, a retry after the quoted message was deleted sent the names as plain text. whisker also no longer lets you edit a message that has mentions, because the edit would send the names as plain text and drop the real mentions.
- A photo you send no longer carries its location, capture time or camera model. Before, whisker uploaded a JPEG, PNG or WebP file with its Exif, XMP and IPTC data, so every recipient could read it. Now the Daemon removes this metadata from every Image before it stores and uploads it. A JPEG keeps its Orientation, so photos do not turn sideways. A file that is not a valid image is refused. A Document stays byte for byte as it is.
- A received voice note can no longer make mpv fetch a link. A file that holds a playlist made mpv open the sender's URL on Play. Now mpv reads every Media file as plain media, only from disk. Audio files now open in mpv too, with the same limits. The file badge of a document now shows its real extension, so a `.desktop` file sent as a PDF no longer shows "PDF".
- A sender can no longer show a link, a tag or bold text in a notification. Omarchy renders the notification text as markup, so a link could show one address and open another. Now whisker inserts an invisible character after each `<` and `&` and into each `://` in the summary and the body.
- Names that other people choose are now cleaned before whisker stores them. This covers Push Names, Phone Contact names, group subjects and document file names. whisker removes newline, control, bidi and zero-width characters, so a name like "Mom" plus blank lines, or one that flips the text direction, can no longer change how the Chat list, the Chat header, the timeline or a notification looks. Emoji, Persian and Indic text, and flags such as England stay as they are. A Push Name has at most 25 characters and a group subject at most 100. An update cleans the names that chat.db already holds.
- Only the author of a message can now delete it for everyone in whisker. Before, any member of a shared chat could make any message, including yours, show as deleted and remove its media.
- A name that a stranger chose for themselves now shows as "~ name +number" in the Chat list, the Chat header and notifications, as it already did in the timeline. Before, anyone could appear as "Mom" there. Names with a phone contact and group names stay as they are.
- Notifications no longer pass message text and names to a helper process, where other local users could read them.
- A member of a group could write a line of JSON into the stream the Plugin reads. A short group Message made libsignal print its raw bytes to stdout. The Plugin then treated that line as a frame. Now only the frame encoder writes to the stdout of the Daemon. The Daemon discards all libsignal log lines.
- A sender can no longer crash the Daemon with a large "separate thumbnail" link. The Daemon stops a thumbnail download at 256 KiB, times it out after 30 seconds, and runs at most three at once. It drops a larger thumbnail, also an inline one, and does not fetch an oversized link again. An update removes oversized thumbnails that chat.db already holds.
- A sender can no longer crash or stall the Daemon with a long mention list. The Daemon now keeps at most 256 different mentions per Message, and only those whose `@<number>` is in the text. It also replaces names in the text in one pass, so a push name that holds `@<number>` cannot grow the text. Before, one small Message could exhaust the memory of the Daemon.
- A member of a group, or the other person in a Direct Chat, can no longer attach Media to the Message of someone else, or to a Message of yours. A Message that reuses the ID of a stored Message from another author is now dropped. It changes no text, Media or Reaction, and it gives no notification and starts no download. A History Sync Message of another author does not replace the stored one either. Before, a modified client could show its file under another name and could hide a real Message.
- A member of a group can no longer change the Disappearing timer of the group for your Messages. The Daemon now ignores the timer setting that a member sends in a group. It takes the group timer only from the group info of the server. Before, a member could turn the timer off, so your Messages stayed, or set it to a few seconds, so the Daemon deleted them.
- The auto-download size limit now holds even when a sender misstates the file size. A file of unknown size (0, or too large to count) now waits for a click, unless the limit is `any`. Every download, automatic or by click, stops and fails once it writes more than the declared size plus encryption overhead, and never more than 2 GiB. At most three files download at once.
- A reply can no longer fake a Quote. Before, a sender could put any words above a reply and pin them on anyone, also on you as "You". Now whisker shows the Quote from the quoted Message that it has stored: its author, its current text, or a deleted-message mark. A Quote of a Message that whisker does not have shows the sender's text with "(unverified)" and never as "You". The contract has a new `verified` key on a Quote.
- A message, a cleared chat or a deleted chat that you remove on your phone is now removed in whisker too. Before, "Delete for me", "Clear chat" and "Delete chat" on another device left the text, the media files, the reactions and the quotes in chat.db and in the Media folder, and the Plugin kept showing them. Now the Daemon deletes them, also when the change arrives during a full sync or after whisker was off, and the Plugin updates the open chat and the chat list.

## [1.0.1] - 2026-10-26

### Changed

- "(edited)" now shows on its own line under an edited Message, in the small, dimmed style of a Quote. Before, it was at the end of the text.

### Fixed

- A Chat that you read on your phone, or on another linked device, now shows as read in whisker. whisker sends no Read Receipt of its own for those Messages.
- With Read Receipts "On interaction", a Chat that you open in whisker now also shows as read on your phone. The sender still gets no Read Receipt until you send a Message or a Reaction.
- The unread count in the Chat list no longer counts Messages that you already saw in whisker. Before, with Read Receipts "On interaction", a Message read in whisker counted again when the next Message came.
- When another person edits a Message, whisker now shows the new text and marks the Message as edited. Before, the edit came in as a new "Unsupported message. Open it on your phone."

## [1.0.0] - 2026-10-26

### Added

- Emoji Shortcodes in the composer. Type ":" and 2 or more letters, such as ":smi", to see the matching emoji in a row above the line. Left and Right select and scroll the row, Tab, Enter or a click inserts, and Escape closes the row. A full code with its closing colon, such as ":thumbsup:", becomes 👍 at once. A ":" inside a word, such as "10:30" or "https://", starts no row. The row shows the code of the selected emoji. The emoji, their codes and the match come from Omarchy's emoji picker (Super+Ctrl+E): ":art" also finds ❤️ "heart". Skin tones are not offered.
- System lines in the timeline. A Group Chat shows who was added, joined using an invite link, left or was removed, who became an admin or no longer is, and who changed the subject, the description or the group icon or created the group. Every Chat shows a change of the disappearing timer. The lines come live and from History Sync, and a change shows once. They raise no notification, set no unread mark and do not move the Chat in the Chat list. A live subject change renames the Chat at once. Messages from `chat.messages` have the new key `system`.
- "load older" asks the phone for older Messages when no stored Message is left (On-Demand History Sync). The link then reads "loading from phone…". The Messages show in time order. The phone sends no answer when it has no older Messages. After 30 s a notice says "No older Messages came from your phone." and the link comes back. A late answer is stored. The Daemon has the new method `chat.requestHistory` and the new notification `historyReceived`.
- The setting "Download media" under Media. Off, images, stickers and voice notes wait for a click. On by default. "Up to" limits it to 1 MB, 5 MB (the default), 25 MB, or no limit; larger files wait for a click. `settings.get` and `settings.set` have the new keys `autoDownload` and `autoDownloadLimit`.
- The linked device shows as "whisker (hostname)" on the phone, so two computers are easy to tell apart. The name applies after a new link.
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
- Ctrl+V in the composer attaches a file that you copied in a file manager, or an image such as a screenshot. A pasted image shows as "Pasted image.png" and waits in memory, never on the disk, until you send or remove it. The Attachment waits in the Draft as "Attached: name" with a remove button (✕). Enter sends it with the text as caption. The Draft keeps it over a Chat switch, closing the pop-out and a Daemon restart. Starting an Edit drops it. Any other clipboard content pastes as text. The paste needs `wl-clipboard`; without it, Ctrl+V pastes text only. The Daemon has the new method `draft.paste` and the errors -32012 and -32013. `draft.set` takes an `attachment` param for the file (a list since the entry under Changed).
- Media shows as a compact chip with WhatsApp's thumbnail (blurred for a video that is not downloaded). An image shows only as the thumbnail of its chip, never at full size. Without WhatsApp's thumbnail, a downloaded image shows its own file small. Chips without a thumbnail show a file-type badge such as IMG, VID, GIF, PDF or MP3. Media whose thumbnail WhatsApp keeps apart, mostly from History Sync, gets it when its Chat opens. A synced image without any thumbnail downloads when its Chat opens, and shows the file as its thumbnail. A later History Sync copy of a Message now fills a thumbnail that the first copy lacked. A GIF says "GIF" and opens in mpv on a loop; the Media object has the field `gif`. A click on the thumbnail or name runs the main action. The chip has download, open and "save as…" links, a progress bar, and a red failure with a retry link. A voice note has a play button and a dot until you play it; the dot stays away after a restart. Audio, voice and video chips show their length. A sticker shows as itself. The Media object has the fields `thumbnail`, `duration` and `played`.
- You can pin, unpin, mute and unmute a Chat, and the change syncs to your phone. Hover a Chat line for the Chat links pin, mark unread and mute. The ⋯ button in the Chat header shows the same links in the header. A click on mute shows the choices 8h, 1w and always in their place. The Chat header shows how long a Muted Chat stays muted. The Daemon has the new methods `chat.pin` and `chat.mute`, and `chats.list` returns `mutedUntil`. A 4th pin shows "You can pin up to 3 chats". An error now shows a short notice in the pop-out, chosen by error code.
- Type a full phone number in the new Chat view to message someone who is not a Phone Contact. A phone number row shows "checking WhatsApp…", then "on WhatsApp · message" (a click opens the Chat) or "not on WhatsApp". A number that is too short or not international, and a failed lookup, show a short message. The lookup waits 400 ms after the last key, so typing does not ask WhatsApp for every digit. The Daemon has the new method `contacts.lookup`.
- Archived Chats no longer show in the Chat list or count as unread. A new "archived" filter lists them. The search field still finds them. An unarchived Chat shows again. When "Keep chats archived" is off on your phone, a new Message unarchives its Chat and notifies. When it is on, the Chat stays archived and silent, unless the Message mentions you. The Daemon has the new `chats.list` param `archived`.

### Changed

- The Chat Sign uses Nerd Font icons: 󰀄 for a Direct Chat, 󰡉 for a Group Chat and 󰃀 for a Self Chat. The Pinned mark is now 󰐃 in place of ▲. On a Chat line, the pin, mute and @ marks moved from after the name to fixed columns before the unread count, so each mark has the same place on every line.
- A Draft holds up to 30 Attachments. Ctrl+V adds the copied files to the end of the list and skips a file that is already there. Each Attachment has its own row with a ✕; after 4 rows the list scrolls. One send puts out one Media Message per Attachment, in order, with the text as caption of the first. If one file cannot be sent, nothing goes out, the Draft stays, and the row of that file shows "Not sendable: name" in red until you remove it. The composer takes back the Draft that the Daemon refused, and keeps text that you typed since. A file that is gone stays in the Draft and shows as not sendable. Starting an Edit or a Quote still drops all Attachments. A paste over 30 shows "30 files at most".
- Contract: `chat.sendMedia` takes `paths` (a list) in place of `path` and answers `{"sent": [...]}`. `draft.set` takes `attachments` in place of `attachment`. `draft.get` returns `attachments` and the new `unsendable`. `draft.paste` returns `pasted` as a list. Error -32012 now means "30 files at most". Error -32013 on `chat.sendMedia` carries `error.data.paths`. A stored Draft with one `attachment` becomes a list of one.
- The "settings" button sits in the pop-out header, next to "+ new chat".
- The pop-out keeps one size in every view. The back button "←" sits at the right of each top bar, so the left side does not shift. The Chat sign or Profile picture sits in a slot with the size and place of the header logo. Chat names in the list, the header and the open Chat start at one position. The pin mark of a Chat follows its name.
- The header shows "● online" while contacts see you Online: whisker is connected and "send online" is on.
- With "send online" off, an open Chat no longer shows Online or Last Seen. WhatsApp may stop sending Presence then, so the line would go stale.
- The settings view has three sections: Privacy, Notifications and Account. Read Receipts is one row of three choices, with a line that says what the set mode does. Send online, Send typing and Notifications are on/off switches, each with a short description. Before, each setting was a separate button.
- The pop-out opens under the whisker icon in the bar, not in the middle of the bar.
- The pop-out follows the prototype (variant C). A run of Messages from one sender has one header line (sender and time) above its Messages, which use the full width; the Delivery State stays at the right end of each own Message, and the hover toolbar floats at its top right. The Chat list has one compact top bar with the unread count, a search row with flat filters, and no "whisker is linked." line. An open Chat has one top bar (back, sign, name, status, pin and mute marks, ⋯), a timeline of fixed height, a quiet "load older" link at its top, and a flat composer strip. The new Chat view has the same bar, search and contact rows.
- Error code -32007 now means "could not reach WhatsApp" (was "send failed"). New codes: -32010 invalid chat, -32011 pin limit and -32602 invalid params.

### Fixed

- A slow clipboard no longer blocks whisker while you paste.
- An image that you send no longer fails while its preview loads. The send and the preview no longer share one plain copy.
- Removing a Reaction from the newest Message no longer scrolls the timeline up to its sender header. The newest Message stays fully in view.
- "load older" and a change to a shown Message no longer scroll the timeline back to the newest Message. A reader who scrolled up keeps the Message at the top edge in place.
- A Message that you send to a Group Chat now moves past `sent`. Its Delivery State is the lowest state that every Recipient reached (`delivered`, `read`, `played`). The Recipients are the Participants of the Group Chat when the send succeeds. A Message from before the update, or from History Sync, keeps its state.
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
- A location has an Open map button that opens the map in the browser. The link in the Message text did not open, and `xdg-open` refused the command.
- History Sync no longer adds a second Chat for a contact that WhatsApp sends by LID. The Daemon uses the phone JID that the conversation carries.
- The linked device shows as "whisker" (a desktop) on the phone, not as "Other device". The name applies after a new link. Users linked with rc.2 keep "Other device" until they link again.
- "Load more" no longer shows in the settings view.

### Removed

- The Attach button and its file picker. Copy the file and press Ctrl+V in the composer instead. whisker no longer needs `QtQuick.Dialogs`.

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
