# Plugin and Daemon contract

This document lists every method and notification between the Plugin and the Daemon.
The golden fixtures in `contract/fixtures/*.jsonl` back it with tests.
The decisions behind it are in [architecture.md](architecture.md), section 7.

## Transport

- The Plugin starts `whisker-daemon` as a child process.
- stdin carries requests. stdout carries frames only. stderr carries logs.
- A frame is one JSON-RPC 2.0 object on one line, ended by `\n`.
- The Daemon rejects a line over 1 MiB (1048576 bytes). It logs an error and exits with code 10.
- The Daemon exits with code 0 on stdin EOF.
- A request without `id` is a notification. The Daemon sends no reply to it.

## Command line

| Call | Result |
|---|---|
| `whisker-daemon --version` | Prints the version from `manifest.json` and exits with code 0. |
| `whisker-daemon` | Runs the contract on stdio. |

## Exit codes

The Go source of truth is `daemon/internal/contract/contract.go`. The Plugin mirrors the codes it maps in `WhiskerState.js`.

| Code | Name | Meaning |
|---|---|---|
| 0 | OK | The Daemon stopped on stdin EOF. |
| 1 | Error | An I/O error. |
| 10 | Oversized line | An input line was over 1 MiB. |
| 11 | Profile in use | Another Daemon holds the Profile lock. The Daemon waits 5 s for it first. |

## Notifications (Daemon to Plugin)

### `ready`

The Daemon sends `ready` once it has opened the Profile (the stores and the store key). The Daemon can take commands from then on.

```json
{"jsonrpc":"2.0","method":"ready","params":{}}
```

Fixture: `contract/fixtures/ready.jsonl`.

If the store key was lost, or the key in the keyring does not open the stores, the Daemon deletes the unreadable Profile first and makes a new key. `ready` then follows as for a new Profile. Fixture: `contract/fixtures/lost-key.jsonl`.

### `keyringUnavailable`

The keyring failed or did not answer in 30 s. The Daemon sends this instead of `ready` and waits for `keyring.retry`. The Plugin shows "keyring unavailable" with Retry.

```json
{"jsonrpc":"2.0","method":"keyringUnavailable","params":{}}
```

Fixtures: `contract/fixtures/keyring-missing.jsonl`, `contract/fixtures/keyring-locked.jsonl`.

### Linking

After `ready`, a Profile that is not linked starts linking. A linked Profile connects at once.

| Notification | Params | Meaning |
|---|---|---|
| `linkQr` | `qr` | A QR code as a `data:image/png;base64,...` URI. A new `linkQr` replaces the old one while the user waits. |
| `linked` | none | The Profile is a Companion Device: sent after the phone authorizes it, and at start-up for a linked Profile. The Plugin leaves the linking screen. |
| `linkFailed` | `reason` | Linking stopped. The Plugin shows the reason with Retry (`link.retry`). |
| `unlinked` | `by` (optional) | The phone removed the Companion Device, or the user chose Log out (`by` is `"user"`; see `profile.logout`). The Daemon has deleted the Profile and its store key. `ready` follows, then linking starts as on a fresh Profile. |

Reasons of `linkFailed`. The Go source of truth is `daemon/internal/whatsapp/whatsapp.go`.

| Reason | Meaning |
|---|---|
| `timeout` | The user did not scan the last QR code in time. |
| `client-outdated` | WhatsApp rejects this whatsmeow version. |
| `scanned-without-multidevice` | The phone scanned the code but multi-device is off. |
| `pair-error` | The phone authorized the link, but pairing failed. |
| `connect-failed` | The Daemon could not connect to WhatsApp. |
| `unexpected` | Any other linking failure. |

Fixtures: `contract/fixtures/link-qr.jsonl`, `link-failed.jsonl`, `link-unlinked.jsonl`, `logout.jsonl`.

### `chatsChanged`

The chat store changed: History Sync brought Chats, or App State, a contact name or a Push Name changed. The Plugin asks for the Chat list again (`chats.list`, offset 0). The Plugin also asks once after `linked`.

```json
{"jsonrpc":"2.0","method":"chatsChanged","params":{}}
```

Fixtures: `contract/fixtures/chats-list.jsonl`, `chats-names.jsonl`, `chats-app-state.jsonl`.

### `chatMerged`

WhatsApp can name a contact by LID in one History Sync batch and give the phone JID in a later one. The Daemon then holds two Chats for one contact. When the port reports the pair (`ChatAlias`), the Daemon merges the LID Chat into the phone Chat and sends `chatMerged`, then `chatsChanged`. Nothing is sent when no LID Chat exists, so a repeated alias is silent.

The merge moves Messages (with their Read Receipt state), Reactions, Media rows and files, and the names of the LID. For a Message id that both Chats hold, the phone Chat copy stays. The phone Chat keeps its Draft. Flags merge: latest `lastActive`, Pinned or unread if either was, the longest mute, and the timer of the phone Chat if it has one. With no phone Chat yet, the LID Chat takes the phone JID. The Attended Chat follows the merge.

The Plugin points the open Chat at `to` when it shows `from` or `to`, empties its Messages, and loads Messages and Draft again.

```json
{"jsonrpc":"2.0","method":"chatMerged","params":{"from":"99@lid","to":"31600@s.whatsapp.net"}}
```

Fixture: `contract/fixtures/chats-merge-lid.jsonl`.

### Connection

The Daemon sends these after `linked`. The Plugin starts as "connecting" after `linked` and shows a "Connecting…" banner until `connected`.

| Notification | Params | Meaning |
|---|---|---|
| `connected` | none | The connection to WhatsApp is up. The Daemon then sends every `pending` Message. |
| `disconnected` | none | The connection is down, or a connect of a linked Profile failed. The Plugin shows "Offline" and dims the bar icon. |

After a failed connect of a linked Profile, the Daemon sends `disconnected` and connects again after 5 s, again and again. WhatsApp reconnects by itself after a connection that was up.

Fixture: `contract/fixtures/connection.jsonl`.

### `messageChanged`

A Message was added or changed: it arrived, the user sent it, or its Delivery State moved. `params.message` is a Message (see `chat.messages`). The Plugin puts it in the open Chat when `message.chat` is that Chat, and replaces a Message with the same `id`. A new Message also brings `chatsChanged`.

```json
{"jsonrpc":"2.0","method":"messageChanged","params":{"message":{"id":"3EB0","chat":"1@s.whatsapp.net","sender":"","senderName":"","fromMe":true,"time":1000,"text":"hi","state":"sent"}}}
```

An edit and a revoke also send `messageChanged`, whether the user, another device or the other Participant made them. See `chat.edit` and `chat.revoke`.

A Reaction, whether the user or another Participant set or removed it, also sends `messageChanged`. See `chat.react`.

A Message can carry two more keys. Both are left out when empty:

- `quote`: the Message that this one replies to, as a snapshot: `{id, sender, senderName, fromMe, text}`. `text` is the preview. `sender` and `senderName` are empty and `fromMe` is true when the quoted Message is the user's.
- `reactions`: a list of `{sender, senderName, fromMe, emoji}`, one per Participant, in the order the Participants first reacted. `sender` is empty for the user.

Later tickets add keys to the Message and send `messageChanged` for each change.

Fixtures: `chat-receive.jsonl`, `chat-send.jsonl`, `chat-offline.jsonl`, `chat-failed.jsonl`, `chat-edit.jsonl`, `chat-revoke.jsonl`.

### `messageExpired`

A Disappearing Message reached the end of its timer. The Daemon has deleted it from the chat store, with its Media. The Plugin takes it out of the open Chat. No `chatsChanged` follows.

```json
{"jsonrpc":"2.0","method":"messageExpired","params":{"chat":"1@s.whatsapp.net","id":"3EB0"}}
```

### Disappearing Messages

A Chat has a timer in seconds (`timer` in `chats.list`, absent when off). It comes from History Sync and from `chatTimer` events, and it applies to Messages from then on. A Message of a timed Chat has `expiresAt` (Unix seconds): the Message time plus the timer. `chat.send` gives the Message of the user the timer of its Chat, and the Daemon sends the timer with it.

The Daemon deletes expired Messages at start-up before `ready`, so a Message that expired while whisker was off is gone at once. After that it sweeps every minute (`contract.ExpiryInterval`) and sends `messageExpired` for each.

Fixtures: `disappearing-expire.jsonl`, `disappearing-restart.jsonl`, `disappearing-send.jsonl`.

### `notify`

A new Message raises a desktop notification. The Daemon sends `notify` after `messageChanged` and `chatsChanged` of that Message. `kind` is the Chat kind, `name` follows the name rule, and `senderName` follows the name rule for Participants. `media` is the Media kind, and only a Message with Media has it.

```json
{"jsonrpc":"2.0","method":"notify","params":{"chat":"g1@g.us","kind":"group","name":"Crew","senderName":"Bob","text":"hello"}}
```

The policy is pure logic in `daemon/internal/notifypolicy`. A Message notifies unless one of these is true:

| Rule | Fixture |
|---|---|
| The notifications setting is off. | `notify-off.jsonl` |
| The Chat is Muted, and the Message does not mention the user. | `notify-muted.jsonl`, `notify-mention-muted.jsonl` |
| The Chat is the Attended Chat. | `notify-attended.jsonl` |
| The user sent the Message from another device. | `notify-own-message.jsonl` |

`notify-new.jsonl` shows a Message that notifies. The Plugin shows the notification with `omarchy-notification-send`. An `--exec` action runs the Plugin IPC handler `openChat` with the Chat id (`omarchy-shell pauwelz.whisker openChat <chat>`). A click opens the pop-out on that Chat. A Message that mentions the user notifies in a Muted Chat too (`mentionsMe` on the port Message), but not in the Attended Chat.

## Methods (Plugin to Daemon)

### `chats.list`

Params, all optional: `offset` (default 0), `limit` (default 50, at most 200) and `query`. Valid once the Daemon is `ready`. The result is one page of the Chat list:

```json
{"chats":[{"jid":"1111@s.whatsapp.net","kind":"direct","name":"Ann","lastActive":100,"pinned":false,"muted":false,"unread":false}],"more":false,"offset":0,"unreadChats":0}
```

- Order: Pinned Chats first, then newest `lastActive` first (Unix seconds). A Chat with no activity is not listed.
- `kind` is `direct`, `group` or `self`.
- `muted` is true for a Muted Chat whose mute has not ended. `unread` is the Read Marker. `pinned`, `muted` and `unread` come from App State.
- `unreadChats` is the number of unread Chats that are not Muted, over the whole chat store. It does not depend on `query` or `offset`. The bar shows it.
- `name` follows the name rule below. `more` is true when another page follows at `offset + limit`.
- `query` keeps the Chats whose `name` contains the text, or in which a Participant with that name wrote a Message. Case does not matter. A blank `query` keeps all Chats. Paging works as without `query`.
- Error -32003 means the chat store failed.

Name rule, resolved by the Daemon at list time:

1. A Group Chat shows its subject.
2. Any other Chat shows the name that the phone contacts hold for the Participant.
3. Otherwise it shows the Push Name.
4. Otherwise it shows the phone number with a leading `+`.

Fixtures: `chats-list.jsonl`, `chats-names.jsonl` (each step of the rule), `chats-pages.jsonl`, `chats-search.jsonl`.

### `settings.get`

No params. Valid once the Daemon is `ready`. The result is the Profile Settings:

```json
{"readReceipts":"onOpen","sendOnline":true,"sendTyping":true,"notifications":true}
```

The Daemon owns the settings in `$XDG_CONFIG_HOME/whisker/settings.json` (default `~/.config/whisker/settings.json`). The file is outside the Profile, so Log out keeps it. The Plugin reads and writes the settings only over JSON-RPC. Later tickets read them through `settings.Store.Get` in `daemon/internal/settings`.

| Setting | Values | Default |
|---|---|---|
| `readReceipts` | `off`, `onOpen`, `onInteraction` | `onOpen` |
| `sendOnline` | boolean | `true` |
| `sendTyping` | boolean | `true` |
| `notifications` | boolean | `true` |

A missing file or a missing key gives the default. Error -32004 means the settings file is unreadable. Fixture: `settings-defaults.jsonl`.

### `settings.set`

Params: any of the settings above. A key that is left out stays as it is. The Daemon saves the file, then answers with the full settings (same shape as `settings.get`). A value that is not allowed (for example an unknown `readReceipts` mode) changes nothing and gives error -32004. Fixtures: `settings-set.jsonl`, `settings-invalid.jsonl`.

### `profile.logout`

No params. Valid only once the Daemon is `ready` and the Profile is linked. The Daemon asks WhatsApp to remove the Companion Device, so the phone no longer lists it. It then answers with an empty `result` and runs the delete-the-Profile routine (`Profile.Delete`). `unlinked` with `by: "user"` and `ready` follow, then linking starts. Profile Settings stay. If WhatsApp cannot be reached, the Daemon logs it and deletes the Profile anyway; the phone then still lists the device until the user removes it there. The Plugin asks the user to confirm before it sends this request. Fixture: `logout.jsonl`.

### `attention.set`

Params: `open` (boolean) and `chat` (JID, or empty for the Chat list). Valid once the Daemon is `ready`. The Plugin sends it when the pop-out opens or closes, when it shows another Chat, and again after each `ready`. The result is empty.

The Daemon derives the **Attended Chat** from it: `chat` while `open` is true, otherwise none. The Go accessor is `attention.Attention` in `daemon/internal/attention`: `Attended()` gives the JID or empty, and `Open()` tells whether the pop-out is open. Notifications (#36) and Presence (#43) read these two. The state survives Log out, but the Chat is cleared with the Profile.

When a Chat becomes attended, or a new Message arrives in it:

1. The Read Marker clears in every mode, locally and on the phone (App State). If it was set, `chatsChanged` follows the reply.
2. Read Receipts go out when the policy says so (`daemon/internal/receiptpolicy`), one call per sender, so each sender in a Group Chat gets their own.

A new Message of another Participant sets the Read Marker (`unread`) of its Chat, then step 1 clears it if the Chat is attended.

| Read Receipts | Read Receipts are sent |
|---|---|
| `off` | Never. |
| `onOpen` | When the Chat becomes attended (all unread Messages), and for each new Message while it stays attended. |
| `onInteraction` | After the user sends a Message (`chat.send`) in the Chat, for all unread Messages at that moment. |

Edit, Revoke, typing and opening Media are not interactions. A Reaction of the user (`chat.react`) and a reply with a Quote (`chat.send` with `quote`) are interactions: both call `reader.interaction`. A Reaction that someone else sets is not. A Read Receipt that fails (offline) stays due and goes out with the next one for that Chat.

Fixtures: `read-on-open.jsonl`, `read-off.jsonl`, `read-on-interaction.jsonl`, `read-group.jsonl`, `read-offline.jsonl`, `attention-closed.jsonl`.

### `chat.messages`

Opens a Chat: it returns one page of its timeline. Params: `chat` (JID), and optional `offset` (default 0) and `limit` (default 50, at most 200). Valid once the Daemon is `ready`. A Chat with no Messages, or an unknown one, gives an empty page.

```json
{"chat":"g1@g.us","messages":[{"id":"m1","chat":"g1@g.us","sender":"2@s.whatsapp.net","senderName":"Bob","fromMe":false,"time":10,"text":"one","state":""}],"more":true,"offset":0}
```

- Offset 0 is the newest page. A page is in time order, oldest first. `more` is true when older Messages exist at `offset + limit`.
- `senderName` follows the name rule for Participants. `media` is the Media kind, and only a Message with Media has it. It is empty for the Messages of the user.
- `state` is the Delivery State of a Message of the user, see below. It is empty for the Messages of others.
- Text that WhatsApp cannot show gets a one-line text, so no Message is blank: `Poll: <name>. Open it on your phone.`, `Event: ...`, `View-once media. ...`, `Contact: <name>, <number>` (one line per card), `Location: <place>` with a map link on the next line, and `Unsupported message. ...`. The port adapter makes these lines. Reactions, poll votes and protocol Messages show nothing. Fixture: `message-types.jsonl`.
- `mentions` lists the mentioned names, each as `@name` (name rule). `text` already holds them: the Daemon replaces `@<number>`. Only Messages with a mention have the key.
- `edited` is true when the text changed after it was sent. `revoked` is true for a Tombstone: its `text` is empty and it keeps its place in the timeline.
- `media` is present only for a Message with Media, see Media below. It never holds a path or bytes.
- Error -32003 means the chat store failed.

Delivery State of a Message of the user:

| State | Meaning |
|---|---|
| `pending` | Stored, not sent yet. It waits for the connection. |
| `sent` | WhatsApp accepted it. |
| `delivered` | The recipient device has it. |
| `read` | The recipient read it. |
| `played` | The recipient played the voice or audio Media. |
| `failed` | The send failed. Retry with `chat.retry`. |

A state only moves forward in this order. Receipts in a Group Chat are not used yet, so a Message there stays `sent`.

Fixtures: `chat-messages.jsonl`, `chat-receive.jsonl`.

### `chat.send`

Params: `chat` (JID of a stored Chat), `text` (not blank, at most 65536 characters) and optional `quote` (the ID of a Message of the same Chat). Valid once the Daemon is `ready` and linked. The Daemon stores the Message as `pending`, answers `{"message": {...}}` at once, and sends it when the connection is up. `messageChanged` reports `sent`, or `failed`.

The Message has its WhatsApp ID from the start. A reconnect or a retry sends that same ID, so a Message never shows twice. Error -32006 means a blank or too long text, an unknown Chat, or a `quote` that is unknown or a Tombstone.

With `quote`, the Message is a reply. The Daemon keeps a snapshot of the quoted Message (author and text) and sends it to WhatsApp with the reply. Incoming replies carry the same `quote` key.

Fixtures: `chat-quote.jsonl`, `chat-send.jsonl`, `chat-offline.jsonl`, `chat-failed.jsonl`.

### `chat.retry`

Params: `chat` and `id` of a `failed` Message. The Daemon makes it `pending`, answers `{"message": {...}}` and sends it. Error -32006 means no such failed Message. Fixture: `chat-failed.jsonl`.

### `chat.sendMedia`

Params: `chat`, `path` (absolute path of a file) and `caption` (optional, at most 65536 characters). Valid once the Daemon is `ready` and linked. The file must be a regular file of 1 byte to 100 MiB. A JPEG, PNG or WebP file goes out as `image`. Every other file goes out as `document` with its file name.

The Daemon encrypts a copy of the file in the Profile (see Media), stores the Message as `pending` with `media.state` `ready`, answers `{"message": {...}}` at once, and uploads it when the connection is up. `messageChanged` reports `sent`, or `failed`. `chat.retry` works as for text. The Message keeps one ID for life. A restart keeps the Media: `media.open` works at once. The copy into the Profile blocks the Daemon for a moment on a large file.

Error -32006 means a relative or unreadable `path`, a file that is empty, too large or not a regular file, a too long caption, or an unknown Chat. Error -32009 means the copy into the Profile failed. Fixtures: `chat-send-media.jsonl`, `chat-send-media-offline.jsonl`.

### `chat.edit`

Params: `chat`, `id` and `text` (not blank, at most 65536 characters). Valid once the Daemon is `ready` and linked. The Message must be a Message of the user that WhatsApp accepted (`sent` or later), not a Tombstone, and at most 15 minutes old. The Daemon sends the edit to WhatsApp, stores the new text with `edited: true`, answers `{"message": {...}}` and sends `messageChanged`. The Message keeps its place.

Error -32006 means the Message cannot be edited (unknown, not the user's, not sent yet, a Tombstone, too old, or a blank or too long text). Error -32007 means WhatsApp could not be reached (offline or a failed call): nothing changed. The call blocks the Daemon for up to 15 s.

An edit from another device, or from another Participant, arrives as `messageChanged` with `edited: true`. The Daemon drops an edit from someone other than the author. Fixture: `chat-edit.jsonl`.

### `chat.react`

Params: `chat`, `id` and `emoji`. Valid once the Daemon is `ready` and linked. The `emoji` is one of the six defaults of WhatsApp (👍 ❤️ 😂 😮 😢 🙏), or empty to remove the Reaction. The Go list is `ReactionEmoji` in `daemon/internal/contract/react.go`; the Plugin mirrors it as `REACTION_EMOJI`.

A Participant has one Reaction per Message, so a new `emoji` replaces the old one. The Message can be anyone's, but not a Tombstone. The Daemon sends the Reaction to WhatsApp, stores it, answers `{"message": {...}}` and sends `messageChanged`.

A Reaction from another device or another Participant arrives as `messageChanged` too. A Reaction on an unknown Message sends nothing. Error -32006 means an unknown or revoked Message or an emoji that is not allowed. Error -32007 means WhatsApp could not be reached: nothing changed. Fixtures: `chat-reaction.jsonl`, `read-reaction-quote.jsonl`.

### `chat.revoke`

Params: `chat` and `id`. Same rules as `chat.edit`, without the 15 minute limit. The Daemon revokes the Message for everyone. It becomes a Tombstone: the store empties the text (and, with Media, the cached Media) and keeps the position. The Daemon answers `{"message": {...}}` and sends `messageChanged` with `revoked: true`.

A revoke from the phone or from the other Participant arrives as `messageChanged` with `revoked: true`. A revoke of an unknown or revoked Message sends nothing. Errors as for `chat.edit`. Fixture: `chat-revoke.jsonl`.

### `draft.get`

Params: `chat`. Valid once the Daemon is `ready`. The result is `{"chat": ..., "draft": {"text", "quote", "edit"}}`. A Draft is the unsent composition of one Chat: its text, the ID of the Message it quotes (`quote`) and the ID of the Message under edit (`edit`). An empty string means none. A Chat with no Draft gives three empty strings. The Daemon drops a `quote` or `edit` whose Message is gone or revoked. The Plugin sends it when it opens a Chat. Fixtures: `draft-chat-switch.jsonl`, `draft-send.jsonl`.

### `draft.set`

Params: `chat`, `text`, `quote` and `edit`. Valid once the Daemon is `ready`. It replaces the Draft of the Chat and answers with an empty result. An empty Draft removes it. The chat store keeps Drafts, so they survive a Daemon restart. The Plugin sends it on each change of the composer.

Error -32006 means a text over 65536 characters, or a `quote` or `edit` that is not a stored Message of the Chat. A successful `chat.send` or `chat.edit` clears the Draft of its Chat. Fixtures: `draft-chat-switch.jsonl`, `draft-send.jsonl`.

### Presence

Presence flows both ways. The Daemon owns the rules; the Plugin only shows and reports.

Incoming (Daemon to Plugin), shown under the Chat header:

| Notification | Params | Meaning |
|---|---|---|
| `presenceChanged` | `chat`, `online`, `lastSeen` | A Participant of a Direct Chat is `Online`, or went away at `lastSeen` (Unix seconds, 0 when WhatsApp hides it). The Daemon subscribes to the Attended Direct Chat to get it. |
| `activityChanged` | `chat`, `participant`, `name`, `state` | `state` is `composing`, `recording` or `paused`. `name` follows the name rule for Participants. The Plugin shows "typing…" or "recording…", with the name in a Group Chat. `paused` ends the activity. |

The Plugin clears all activity on `disconnected`.

Outgoing (Plugin to Daemon):

- `chat.typing` has params `chat` and `typing` (boolean). Valid once the Daemon is `ready` and linked. The Daemon sends Composing (true) or Paused (false) to the Chat, only if "send typing" is on. The result is empty. The Plugin sends it when the composer gets or loses text, and when the Chat or the pop-out closes.
- `Online` is not a method. The Daemon derives it: it sends `Online` while `attention.Open()` is true and "send online" is on, and otherwise sends unavailable. It follows the pop-out, not the shell. It recomputes on `attention.set`, on `settings.set` and after each `connected` (WhatsApp needs the availability once per connection). A user who turns "send online" off may also stop receiving Presence from WhatsApp.

Fixtures: `presence-incoming.jsonl`, `presence-online.jsonl`, `presence-typing.jsonl`.

### Media

A Message with Media has a `media` object: `{"kind","mime","name","size","state"}`. `text` holds the caption. `size` is in bytes. `name` is the file name of a document, else empty.

| `kind` | Download |
|---|---|
| `image`, `sticker`, `voice` | Automatic, when the Message arrives. |
| `video`, `audio`, `document` | On `media.download`. |

The automatic rule is fixed. Media from History Sync never downloads by itself.

| `state` | Meaning |
|---|---|
| `none` | Not downloaded. |
| `downloading` | The download runs. |
| `ready` | Stored in the Profile, encrypted. `media.open` and `media.save` work. |
| `failed` | The download failed. `media.download` tries again. |

Every state change sends `messageChanged`. A revoked Message (Tombstone) has no `media`, and its stored file is deleted. The Daemon sets Media that was `downloading` at a restart back to `none`.

The Daemon stores received Media encrypted again in the Profile, in WhatsApp's CDN format (see architecture, section 5). It makes a plain copy only on `media.open`, in `$XDG_RUNTIME_DIR/whisker/media/<chat>/`. It checks the HMAC first. A changed byte gives error -32009 and no copy. It deletes the copies of a Chat on `media.release`, and all copies at start and at exit. Logs never hold a Chat, a name or a path.

#### `media.download`

Params: `chat` and `id`. Valid for Media in state `none` or `failed`. The Daemon answers `{"message": {...}}` with state `downloading`, then sends `messageChanged` with `ready` or `failed`. Error -32008 means no such Media, or it is already `downloading` or `ready`.

#### `media.open`

Params: `chat` and `id`. Valid for Media in state `ready`. The result is `{"chat","id","path"}`: the path of a plain copy. Error -32008 means no such Media, or it is not `ready`. Error -32009 means the integrity check failed, or the copy failed.

#### `media.save`

Params: `chat`, `id` and `path` (absolute). The Daemon writes a plain copy to `path` (mode 0600) and answers an empty `result`. This is an explicit export: the Daemon does not delete it. Errors as for `media.open`, and -32008 for a relative `path`.

#### `media.played`

Params: `chat` and `id`. The Plugin sends it when a voice note starts to play. Valid for a `ready` voice note of another Participant. Error -32008 means anything else. The Daemon answers an empty `result` and the policy (`receiptpolicy.Played`) decides what goes out:

| Read Receipts | Sent |
|---|---|
| `off` | Nothing. |
| `onOpen` | `Played`. Read went out when the Chat became attended. |
| `onInteraction` | `Read` and `Played` for that Message only. The rest of the Chat stays unread for the sender. |

A repeat play of the same note sends nothing (the Daemon remembers until restart). A receipt that fails (offline) is logged and sent again at the next play.

Fixtures: `played-off.jsonl`, `played-on-open.jsonl`, `played-on-interaction.jsonl`, `played-invalid.jsonl`.

#### `media.release`

Params: `chat`. The Daemon deletes the plain copies of that Chat and answers an empty `result`. The Plugin sends it when the Chat closes.

Fixtures: `media-auto.jsonl`, `media-open.jsonl`, `media-click.jsonl`, `media-revoke.jsonl`.

### `link.pairCode`

Params: `phone` (digits, international, no `+`). Valid only while the Profile is not linked. The result is `{"code": "ABCD-1234"}`: the Pair Code as text. The user types it on the phone. Success then follows as `linked`. Errors: -32001 and -32002 (see Errors). Fixtures: `link-pair-code.jsonl`, `link-pair-code-invalid.jsonl`.

### `link.retry`

Valid only while the Profile is not linked. The Daemon answers with an empty `result`, then connects again. A new `linkQr` follows, or `linkFailed`. Fixture: `link-failed.jsonl`.

### `keyring.retry`

Valid only after `keyringUnavailable`. The Daemon answers with an empty `result` at once. Then it joins the keyring attempt that is still in flight (or starts one) and sends `ready` or `keyringUnavailable` again. Any other time, the method does not exist.

## Errors

The Daemon answers a request with an `error` object. Application codes will join the standard ones here.

| Code | Message | When |
|---|---|---|
| -32700 | parse error | The line is not a JSON-RPC object. The `id` is `null`. |
| -32601 | method not found | The method does not exist, or is not valid now (for example `link.pairCode` on a linked Profile). |
| -32001 | invalid phone number | `link.pairCode` got a number that is too short or not international. |
| -32002 | pair code failed | WhatsApp did not give a Pair Code. |
| -32003 | chat store failed | `chats.list`, `chat.messages`, `chat.send` or `chat.retry` could not use the chat store. |
| -32004 | invalid setting | `settings.set` got a value that is not allowed, or the settings file could not be read or written. |
| -32006 | invalid message | `chat.send` or `chat.sendMedia` got a blank or too long text or an unknown Chat. `chat.retry` found no failed Message. `chat.edit` or `chat.revoke` cannot change that Message. `chat.react` got an unknown or revoked Message or an emoji that is not allowed. `chat.send` got a `quote` that is unknown or revoked. |
| -32007 | send failed | `chat.edit`, `chat.revoke` or `chat.react` could not reach WhatsApp. The Message is unchanged. |
| -32008 | invalid media | `media.download`, `media.open`, `media.save`, `media.played` got Media that does not exist or is in the wrong state, or a relative `path`. |
| -32009 | media failed | `media.open` or `media.save` failed the integrity check, or could not write the copy. |

Fixture: `contract/fixtures/unknown-method.jsonl`.

## Chat store schema

The chat store (`chat.db`) is versioned by `PRAGMA user_version`. The migrations are the SQL files in `daemon/internal/chatstore/migrations/`.

To change the schema (a new table or column):

1. Add `NNNN_name.sql` with the next free number. Write plain SQL.
2. Do not edit a file that a release has shipped. Add a new one.
3. Two branches can pick the same number. Rename one file when you merge.

`chatstore.Migrate` runs each missing file in order, each in one transaction. `profile.Open` calls it. A store from a newer Daemon makes `Open` fail.

## Golden fixtures

A fixture is a JSON Lines file. Each line is one object with a `dir` key:

| `dir` | Other keys | Meaning |
|---|---|---|
| `in` | `frame` | The Plugin sends `frame` to the Daemon. A string `frame` is sent as a raw line. |
| `out` | `frame` | The Daemon sends `frame`. |
| `view` | `expect` | After the `out` frames above it, the Plugin view state holds every key of `expect`. |
| `keyring` | `state` | The Go test sets the fake keyring: `missing` (calls fail), `ok`, `locked` (calls block) or `unlocked`. |
| `profile` | `state` | The Go test, before the start: `unreadable` leaves old stores with no key. |
| `read` | `receipts`, `plays`, `markers` | The Go test checks the Read Receipts and the Played receipts (`chat`, `sender`, `ids`) and the Chats whose Read Marker the fake port cleared, all so far, in order. |
| `clock` | `advance` | The Go test fires the oldest keyring timeout (30 s). |
| `sent` | `ids`, `timers` (optional) | The Go test checks the Message IDs that the fake port has sent so far, in order. With `timers`, it also checks the Disappearing timer of each, in seconds. |
| `edited` | `edits` | The Go test checks the edits that the fake port has sent so far, in order, as `"id:text"`. |
| `reacted` | `reactions` | The Go test checks the Reactions that the fake port has sent so far, in order, as `"id:emoji"`. An empty emoji is a removal. |
| `quoted` | `quotes` | The Go test checks the replies that the fake port has sent with a Quote so far, in order, as `"id:quotedID"`. |
| `revoked` | `ids` | The Go test checks the Message IDs that the fake port has revoked so far, in order. |
| `uploaded` | `uploads` | The Go test checks the Media that the fake port has uploaded and sent so far, in order, as `"id\|kind\|mime\|name\|caption\|content"`. |
| `clock` | `now` | With `now` (Unix seconds), the Go test sets the Daemon time and fires the oldest sweep timer of Disappearing Messages. Without it, the line fires the keyring timeout as above. |
| `restart` | `now` (optional) | The Go test stops the Daemon like a closed stdin and starts it again on the same Profile, with a new fake WhatsApp port. `now` sets the Daemon time while it was off. Put the `whatsapp` start-up lines after it. |
| `file` | `path`, and `content` or `missing` | With `write`: true the Go test writes the file with `content` (a source file for `chat.sendMedia`). Otherwise the Go test checks a file: it holds `content`, or (`missing`: true) it does not exist, or (`present`: true) it exists. `$PROFILE` in `path` is the Profile folder. `$RUNTIME` in `path` (and in `in` and `out` frames) is the runtime folder of the test. The Plugin test ignores it. |
| `plugin` | `openChat` | The Plugin test opens this Chat (`jid`, `name`, `kind`) as the user does. The Go test ignores it. |
| `whatsapp` | one of the keys below | The Go test scripts the fake WhatsApp port. |

Keys of a `whatsapp` line. Each line has exactly one:

| Key | Meaning |
|---|---|
| `linked` | `true` or `false`: the Profile is linked at start-up. Put the line before the first `out`. |
| `pairCode` | The code that the next `link.pairCode` returns. |
| `pairError` | `invalid-phone`, or any text: the next `link.pairCode` fails. |
| `sendError` | `offline`, any other text, or empty: the fake port fails every send, edit and revoke with that error, or works again (empty). |
| `download` | The plain Media that the next downloads write. |
| `downloadError` | Any text, or empty: the fake port fails every download with that error, or works again (empty). |
| `readError` | `offline`, or empty: the fake port fails every Read Receipt, or works again (empty). |
| `event` | The fake port plays this event now (also `presence` and `chatPresence`, with the JSON keys of their type). `kind` is `qr` (with `code`), `pairSuccess`, `linkFailed` (with `reason`), `loggedOut`, `connected` or `disconnected`. |

The `event` kinds `historySync`, `pushName`, `contactName`, `pin`, `mute` and `readMarker` carry the JSON keys of their type in `daemon/internal/whatsapp/whatsapp.go`. Keys that a fixture leaves out take their zero value. `mutedUntil` and `until` are Unix seconds: 0 is not muted and -1 is muted for ever.

The `event` kinds `newMessage`, `receipt`, `messageEdited`, `messageRevoked` and `reaction` also carry the JSON keys of their type. The fixtures fix the Message time of the Daemon at 1000 (Unix seconds).

The `event` kind `chatAlias` (`lid`, `phone`) tells the Daemon that a LID and a phone JID are one contact.

The `event` kind `chatTimer` (`chat`, `seconds`) sets the Disappearing timer of a Chat. A `newMessage` takes `timer` (seconds), and a `historySync` Chat takes `timer`.

The events are the event kinds of the WhatsApp port (`daemon/internal/whatsapp`). A new port event kind gets a new `event` kind here. A new port method that a fixture must script gets a new key.

- The Go test (Seam A) runs the lines in order: it sends each `in` line and waits for each `out` frame. At the end it checks that no frame is left. It ignores `view` lines.
- The `node --test` test (Seam B) folds the `out` frames through `WhiskerState.js` and checks each `view` line. It ignores `in` lines.
- Later tickets add line kinds here, in this table.

### Add a fixture

1. Add a row for the method or notification to this document.
2. Write `contract/fixtures/<name>.jsonl` with `in`, `out` and `view` lines.
3. Run both test runners. Both must fail for the new behaviour.
4. Implement the Daemon behaviour until the Go test passes.
5. Implement the Plugin state logic until the `node --test` test passes.

### Run the tests

```sh
cd daemon && go test ./...
node --test tests/*.test.js
```
