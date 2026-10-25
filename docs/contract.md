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

The chat store changed: History Sync brought Chats, or App State, a contact name or a Push Name changed. The Plugin asks for the Chat list again (`chats.list`, offset 0). The Plugin also asks once after `linked`. The Daemon also sends it when the earliest Mute ends, because the unread count then changes.

```json
{"jsonrpc":"2.0","method":"chatsChanged","params":{}}
```

Fixtures: `contract/fixtures/chats-list.jsonl`, `chats-names.jsonl`, `chats-app-state.jsonl`, `chats-mute-ends.jsonl`, `chats-history-after-live.jsonl`, `chats-archived.jsonl`.

History Sync fills `pinned`, `archived`, `muted`, `unread` and `timer` of a Chat only until a live event has set that field.

App State also holds the setting "Keep chats archived". The Daemon stores it in the chat store. When it is off, a new live Message (incoming or sent by another device) unarchives its Chat and sets the live field `archived`, so a later History Sync does not archive the Chat again. When it is on or unknown, the Chat stays archived.

### `chatMerged`

WhatsApp can name a contact by LID in one History Sync batch and give the phone JID in a later one. The Daemon then holds two Chats for one contact. When the port reports the pair (`ChatAlias`), the Daemon merges the LID Chat into the phone Chat and sends `chatMerged`, then `chatsChanged`. Nothing is sent when no LID Chat exists, so a repeated alias is silent.

The port reports a pair from History Sync, from the Chat of a live event, and from the author of a live event and its alt ID. In a Group Chat the author pair renames the older Messages of that Participant. On each connect the Daemon also asks the local LID store for every stored LID, and merges each one it knows. This fixes Messages that arrived before WhatsApp gave the phone JID.

The merge moves Messages (with their Read Receipt state), Reactions, Media rows and files, and the names of the LID. For a Message id that both Chats hold, the phone Chat copy stays. If both Chats hold a Draft, the merged Draft keeps the phone Quote, Edit and file, and joins the texts: phone text, a line break, LID text. A lone Draft moves over. The merge runs when the LID shows in any stored row: Chat, name, Draft, Message (Chat, author or quoted author) or Reaction. Flags merge: latest `lastActive`, Pinned or unread if either was, the longest mute, and the timer of the phone Chat if it has one. With no phone Chat yet, the LID Chat takes the phone JID. The Attended Chat follows the merge. A send that began in the LID Chat and ends after the merge updates the Message in the phone Chat (fixture `chat-send-merge.jsonl`).

The Plugin points the open Chat at `to` when it shows `from` or `to`, empties its Messages, and loads Messages and Draft again.

```json
{"jsonrpc":"2.0","method":"chatMerged","params":{"from":"99@lid","to":"31600@s.whatsapp.net"}}
```

Fixtures: `contract/fixtures/chats-merge-lid.jsonl`, `contract/fixtures/chats-lid-sender-repair.jsonl`.

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
- `reactions`: a list of `{sender, senderName, fromMe, emoji}`, one per Participant, in the order the Participants first reacted. `sender` is empty for the user. History Sync carries the Reactions of older Messages; a Reaction that a live event set wins over a later History Sync batch. Fixture: `chats-history-reactions.jsonl`.

Later tickets add keys to the Message and send `messageChanged` for each change.

Fixtures: `chat-receive.jsonl`, `chat-send.jsonl`, `chat-offline.jsonl`, `chat-failed.jsonl`, `chat-edit.jsonl`, `chat-revoke.jsonl`.

### `messageExpired`

A Disappearing Message reached the end of its timer, or `chat.deleteForMe` deleted it. The Daemon has deleted it from the chat store, with its Media. The Plugin takes it out of the open Chat. No `chatsChanged` follows.

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
| The Chat stays archived after the Message ("Keep chats archived" is on or unknown), and the Message does not mention the user. A Message that unarchives its Chat notifies. | `chats-archived.jsonl` |
| The Chat is the Attended Chat. | `notify-attended.jsonl` |
| The user sent the Message from another device. | `notify-own-message.jsonl` |

`notify-new.jsonl` shows a Message that notifies. The Plugin shows the notification with `omarchy-notification-send`. An `--exec` action runs the Plugin IPC handler `openChat` with the Chat id (`omarchy-shell pauwelz.whisker openChat <chat>`). A click opens the pop-out on that Chat. A Message that mentions the user notifies in a Muted Chat too (`mentionsMe` on the port Message), but not in the Attended Chat.

## Methods (Plugin to Daemon)

### `chats.list`

Params, all optional: `offset` (default 0), `limit` (default 50, at most 200), `query` and `archived` (boolean, default false). Valid once the Daemon is `ready`. The result is one page of the Chat list:

```json
{"chats":[{"jid":"1111@s.whatsapp.net","kind":"direct","name":"Ann","lastActive":100,"pinned":false,"muted":false,"unread":false}],"more":false,"offset":0,"unreadChats":0}
```

- Order: Pinned Chats first, then newest `lastActive` first (Unix seconds). A Chat with no Message and no Draft is not listed.
- `kind` is `direct`, `group` or `self`.
- Without `query`, the list holds the archived Chats if `archived` is true, and all other Chats if not. With a `query`, `archived: false` searches all Chats, also the archived ones, and `archived: true` searches only the archived Chats. A Chat has `archived: true` when it is archived. The field is absent otherwise.
- `muted` is true for a Muted Chat whose mute has not ended. `unread` is the Read Marker. `pinned`, `muted` and `unread` come from App State.
- `mutedUntil` is the end of the mute in Unix seconds, `-1` for ever. It is present only when `muted` is true.
- `unreadCount` is the number of live Messages that wait for a Read Receipt in an unread Chat. It is absent (0) for a read Chat. A Chat that the phone marked unread has none, so the Plugin shows a dot. `mention` is true when one of those Messages mentions the user. It is absent otherwise.
- `unreadChats` is the number of unread Chats that are not Muted or archived, and that the list shows (a Chat with no Message and no Draft is not listed), over the whole chat store. It does not depend on `query` or `offset`. The bar shows it.
- `name` follows the name rule below. `more` is true when another page follows at `offset + limit`.
- `query` keeps the Chats whose `name` contains the text, or in which a Participant with that name wrote a Message. Case does not matter. A blank `query` keeps all Chats. Paging works as without `query`.
- Error -32003 means the chat store failed.

Name rule, resolved by the Daemon at list time:

1. A Group Chat shows its subject.
2. Any other Chat shows the name that the phone contacts hold for the Participant.
3. Otherwise it shows the Push Name.
4. Otherwise it shows the phone number with a leading `+`.

The names of the phone contacts come from App State. whatsmeow sends no event for a full App State sync, so on each connect and after each full sync the Daemon reads every named contact from the device store and stores the names under the phone JID. This read never removes a name; a live App State change does.

Fixtures: `chats-list.jsonl`, `chats-names.jsonl` (each step of the rule), `chats-pages.jsonl`, `chats-search.jsonl`, `chats-contact-book.jsonl`.

### `contacts.search`

Params, all optional: `query` and `limit` (default 50, at most 200; there is no paging). Valid once the Daemon is `ready`. The result lists people from the names that the chat store holds:

```json
{"contacts":[{"jid":"1111@s.whatsapp.net","name":"Ann","phone":"+1111","pushNameOnly":false,"chat":"1111@s.whatsapp.net"}]}
```

- Order: Phone Contacts by name (case does not matter), then Participants known only by a Push Name (`pushNameOnly` is true), by name.
- `phone` is the phone number with a leading `+`. It is empty for a LID.
- `chat` is the WhatsApp ID of the stored Chat with the person, also an empty Chat that `chat.start` made. It is empty when there is none.
- `query` keeps the people whose `name` contains the text, or whose phone number holds its digits. A blank `query` keeps all, so it returns the first `limit` Phone Contacts.
- The user's own IDs (phone and LID) are never returned.
- Error -32003 means the chat store failed.

Fixture: `contacts-search.jsonl`.

### `contacts.lookup`

Params: `phone`, a phone number in international form. Digits are kept, so `+1 555-123-4567` is fine. Valid once the Daemon is `linked`. The Daemon asks WhatsApp if the number is on WhatsApp. The result is `{"phone": "15551234567", "jid": "15551234567@s.whatsapp.net"}`, or `{"phone": "15551234567", "jid": ""}` when the number is not on WhatsApp. `phone` always holds the digits that the reply is for, as the Daemon cleaned them. This echo is on purpose: the Plugin ignores a reply whose `phone` is not the number in its row, so a late reply cannot change the row of a number the user has edited since.

- Error -32001 means the number is too short (6 digits or fewer) or not international (it starts with 0). `link.pairCode` uses the same rule.
- Error -32007 means WhatsApp could not be reached: no connection or a timeout. The call blocks the Daemon for up to 15 s.

Fixtures: `contacts-lookup.jsonl`, `contacts-lookup-invalid.jsonl`, `contacts-lookup-offline.jsonl`.

### `chat.start`

Params: `jid`, a person's WhatsApp ID in phone (`digits@s.whatsapp.net`) or LID (`digits@lid`) form. Valid once the Daemon is `ready`. The result is `{"chat": <Chat>}`, a row as in `chats.list`. The Daemon returns the stored Chat, or stores a new empty one. It makes no call to WhatsApp. An empty Chat stays out of `chats.list` until it has a Message or a Draft.

- Error -32010 means `jid` is not a person's ID: a Group Chat, a malformed ID or one with a device part.
- Error -32003 means the chat store failed.

Fixtures: `chat-start.jsonl`, `chat-start-invalid.jsonl`.

### `settings.get`

No params. Valid once the Daemon is `ready`. The result is the Profile Settings:

```json
{"readReceipts":"onOpen","sendOnline":true,"sendTyping":true,"notifications":true,"autoDownload":true,"autoDownloadLimit":"5mb"}
```

The Daemon owns the settings in `$XDG_CONFIG_HOME/whisker/settings.json` (default `~/.config/whisker/settings.json`). The file is outside the Profile, so Log out keeps it. The Plugin reads and writes the settings only over JSON-RPC. Later tickets read them through `settings.Store.Get` in `daemon/internal/settings`.

| Setting | Values | Default |
|---|---|---|
| `readReceipts` | `off`, `onOpen`, `onInteraction` | `onOpen` |
| `sendOnline` | boolean | `true` |
| `sendTyping` | boolean | `true` |
| `notifications` | boolean | `true` |
| `autoDownload` | boolean | `true` |
| `autoDownloadLimit` | `1mb`, `5mb`, `25mb`, `any` | `5mb` |

`autoDownload` on: images, stickers and voice notes download when they arrive, and a listed image without a thumbnail downloads. Off: they wait for `media.download`. `autoDownloadLimit` is the largest file that downloads this way; a larger one waits, and a file of unknown size downloads. WhatsApp's separate thumbnails download either way.

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
- `mentionsMe` is true when the Message mentions the user. Only such Messages have the key. Fixture: `notify-mention-muted.jsonl`.
- `senderPush` is true when `senderName` is a Push Name: the user has no phone contact for the sender. Only such Messages have the key. The Plugin shows "~" and the phone number from `sender`.
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

A state only moves forward in this order. In a Group Chat, a Message of the user has the lowest state that every Recipient reached (the Participants of the Group Chat when the send succeeded). A Message from History Sync keeps the state that History Sync carries.

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

### `chat.markUnread`

`chat.markUnread` takes `chat` and `unread` (boolean). It sets or clears the Read Marker of the Chat as an App State write. It sends no Read Receipt and no Played receipt. Valid once the Daemon is `ready` and linked.

The store changes only after WhatsApp accepts the write. Then the Daemon answers `{}` and sends `chatsChanged`. Error -32010 means an unknown Chat. Error -32007 means WhatsApp could not be reached: nothing changes. The write does not change the Attended Chat. The Plugin leaves the open Chat before it marks it unread, so the mark stays. Fixture: `chat-mark-unread.jsonl`.

### `chat.picture`

Params: `chat`. Valid once the Daemon is `ready` and linked. The result is `{"chat": "<jid>", "data": "<base64 JPEG>"}`. `data` is `""` when the Chat has no picture or WhatsApp hides it. The picture is the small preview of a contact's Profile picture or of a group icon.

The Daemon keeps the picture in the encrypted chat store. A cache younger than 24 hours answers with no call to WhatsApp. An older cache makes the Daemon ask WhatsApp with the cached picture ID: an unchanged picture only renews the cache time, a changed one replaces the cache. Without a connection, or when the call fails, the Daemon answers from the cache (`""` when there is none) and no error. The call blocks the Daemon for up to 15 s.

- Error -32010 means an unknown Chat.
- Error -32003 means the chat store failed.

The `pictureChanged` notification tells the Plugin to ask again. Fixture: `chat-picture.jsonl`.

### `chat.pin` and `chat.mute`

`chat.pin` takes `chat` and `pinned` (boolean). `chat.mute` takes `chat` and `until`, in Unix seconds: `-1` mutes for ever, `0` unmutes, any other value must be in the future. Valid once the Daemon is `ready` and linked.

The Daemon writes App State to WhatsApp, as the Read Marker write does. The store changes only after WhatsApp accepts the write. Then the Daemon answers `{}` and sends `chatsChanged`. Without a connection the call fails at once, nothing changes and nothing is queued.

- Error -32010 means an unknown Chat. It comes before the other checks.
- Error -32011 means `chat.pin` would make a 4th Pinned Chat. Pinning a Chat that is already pinned is allowed.
- Error -32602 means `chat.mute` got no `until`, or one that is not `-1`, `0` or in the future.
- Error -32007 means WhatsApp could not be reached. Nothing changed.

The Plugin turns a mute choice into an absolute `until`. Fixtures: `chat-pin.jsonl`, `chat-mute.jsonl`, `chat-options-offline.jsonl`.

### `chat.revoke`

Params: `chat` and `id`. Same rules as `chat.edit`, without the 15 minute limit. The Daemon revokes the Message for everyone. It becomes a Tombstone: the store empties the text (and, with Media, the cached Media) and keeps the position. The Daemon answers `{"message": {...}}` and sends `messageChanged` with `revoked: true`.

A revoke from the phone or from the other Participant arrives as `messageChanged` with `revoked: true`. A revoke of an unknown or revoked Message sends nothing. Errors as for `chat.edit`. Fixture: `chat-revoke.jsonl`.

### `chat.deleteForMe`

Params: `chat` and `id`. Valid once the Daemon is `ready` and linked. It deletes one Message on the user's devices only, also a Tombstone, as an App State write (`deleteMessageForMe`). Other Participants keep the Message.

The store changes only after WhatsApp accepts the write: the Message goes with its Media file and its Reactions. Then the Daemon answers `{}` and sends `messageExpired` for it, which takes it out of the open Chat. Error -32010 means an unknown Chat. Error -32006 means an unknown Message. Error -32007 means WhatsApp could not be reached: nothing changes. Fixture: `chat-delete-for-me.jsonl`.

### `draft.get`

Params: `chat`. Valid once the Daemon is `ready`. The result is `{"chat": ..., "draft": {"text", "quote", "edit", "attachment"}}`. A Draft is the unsent composition of one Chat: its text, the ID of the Message it quotes (`quote`), the ID of the Message under edit (`edit`) and the absolute path of a staged file (`attachment`). An empty string means none. A Chat with no Draft gives four empty strings. The Daemon drops a `quote` or `edit` whose Message is gone or revoked, and an `attachment` whose file is gone or no longer a regular file. The text stays. The Plugin sends it when it opens a Chat. Fixtures: `draft-chat-switch.jsonl`, `draft-send.jsonl`, `draft-attachment.jsonl`, `draft-attachment-gone.jsonl`.

### `draft.set`

Params: `chat`, `text`, `quote`, `edit` and `attachment`. Valid once the Daemon is `ready`. It replaces the Draft of the Chat and answers with an empty result. An empty Draft removes it. The chat store keeps Drafts, so they survive a Daemon restart. The Plugin sends it on each change of the composer.

Error -32006 means a text over 65536 characters, or a `quote` or `edit` that is not a stored Message of the Chat. It also means a new `attachment` that `chat.sendMedia` would refuse (not absolute, not a regular file, empty or over 100 MiB; the Daemon checks it with `stat`, never `open`), or an `attachment` together with an `edit`. The Daemon checks the file only when `attachment` differs from the stored one, so a keystroke that keeps a file that is gone now still saves. A staged file and an Edit never combine: the Plugin drops the file when an Edit starts, and sends no `draft.paste` during an Edit. A file that is gone at send time gives the error of `chat.sendMedia`. A successful `chat.send`, `chat.sendMedia` or `chat.edit` clears the Draft of its Chat. Fixtures: `draft-chat-switch.jsonl`, `draft-send.jsonl`, `draft-attachment.jsonl`, `draft-attachment-gone.jsonl`.

### `draft.paste`

Params: `chat`. Valid once the Daemon is `ready`. The Plugin sends it on Ctrl+V in the composer, except during an Edit. The Daemon reads the clipboard with `wl-paste` and answers `{"chat": ..., "pasted": ...}`. It does not change the Draft.

- `pasted` is the absolute path of the one `file://` URI in `text/uri-list`, as a file manager copies a file. The Plugin then stages it as the `attachment` with `draft.set`.
- With no `file://` URI, an image wins over text. For `image/png`, `image/jpeg` or `image/webp` (best first), the Daemon writes the bytes to `$XDG_RUNTIME_DIR/whisker/pasted/<random>/Pasted image.<ext>` (folder 0700, file 0600) and `pasted` is that path. The folder is in memory, so the plain image never reaches the disk.
- The Daemon deletes a pasted image, with its folder, when `draft.set` changes the `attachment` away from it, when `chat.sendMedia` has stored its encrypted copy, and on an unlink. It never deletes an `attachment` outside that folder. There is no sweep at start; a reboot clears the folder.
- `pasted` is empty when the clipboard has no `file://` URI and no image, when `wl-paste` is not installed, or when a `wl-paste` call takes more than 2 seconds. The Plugin then pastes the text of the clipboard itself. A missing `wl-paste` is logged once.
- Error -32012 means two or more `file://` URIs. Error -32013 means a file or image that `chat.sendMedia` would refuse: not a regular file, empty or over 100 MiB.

The `chat` param only comes back in the result, so the Plugin drops a reply for a Chat that is no longer open.

### Presence

Presence flows both ways. The Daemon owns the rules; the Plugin only shows and reports.

Incoming (Daemon to Plugin), shown under the Chat header:

| Notification | Params | Meaning |
|---|---|---|
| `pictureChanged` | `chat` | The Profile picture of a Chat changed or went away. The Daemon dropped its cached picture. The Plugin asks again with `chat.picture` when the Chat is open. |
| `presenceChanged` | `chat`, `online`, `lastSeen` | A Participant of a Direct Chat is `Online`, or went away at `lastSeen` (Unix seconds, 0 when WhatsApp hides it). The Daemon subscribes to the Attended Direct Chat to get it. |
| `activityChanged` | `chat`, `participant`, `name`, `state` | `state` is `composing`, `recording` or `paused`. `name` follows the name rule for Participants. The Plugin shows "typing…" or "recording…", with the name in a Group Chat. `paused` ends the activity. |

The Plugin clears all activity on `disconnected`.

Outgoing (Plugin to Daemon):

- `chat.typing` has params `chat` and `typing` (boolean). Valid once the Daemon is `ready` and linked. The Daemon sends Composing (true) or Paused (false) to the Chat, only if "send typing" is on. The result is empty. The Plugin sends it when the composer gets or loses text, and when the Chat or the pop-out closes.
- `Online` is not a method. The Daemon derives it: it sends `Online` while `attention.Open()` is true and "send online" is on, and otherwise sends unavailable. It follows the pop-out, not the shell. It recomputes on `attention.set`, on `settings.set` and after each `connected` (WhatsApp needs the availability once per connection). A user who turns "send online" off may also stop receiving Presence from WhatsApp.

Fixtures: `presence-incoming.jsonl`, `presence-online.jsonl`, `presence-typing.jsonl`.

### Media

A Message with Media has a `media` object: `{"kind","mime","name","size","state"}` and, when they apply, `"thumbnail"`, `"duration"`, `"played"` and `"gif"`. `text` holds the caption. `size` is in bytes. `name` is the file name of a document, else empty. `thumbnail` is the small JPEG that WhatsApp sends with image, video and document Messages, as base64, passed on as it arrives. The Daemon makes no thumbnails. WhatsApp sometimes keeps the thumbnail apart from the Message, mostly in History Sync. The Daemon then downloads it once `chat.messages` lists the Message, stores it, and sends `messageChanged` with it. A failed download is not tried again until the Daemon restarts. History Sync often sends an image with no thumbnail at all. When `chat.messages` lists such an image and it is not downloaded, the Daemon downloads it, as it does a live image. The Plugin then shows the file as the thumbnail. The key is absent when WhatsApp sent none. `duration` is the length in seconds of audio, voice and video, absent when WhatsApp sent none. `played` is true once the user played the voice note in whisker, absent before. `gif` is true for a video that WhatsApp plays as a looping GIF, absent otherwise. Its `kind` stays `video`. Fixtures: `media-thumbnail.jsonl`, `played-on-open.jsonl`.

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

Params: `chat` and `id`. The Plugin sends it when a voice note starts to play. Valid for a `ready` voice note of another Participant. Error -32008 means anything else. The Daemon stores `played` on the Media, whatever the policy says. The first play sends `messageChanged` for the Message, a repeat sends nothing. The Daemon answers an empty `result` and the policy (`receiptpolicy.Played`) decides what goes out:

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
| -32602 | invalid params | `chat.mute` got an `until` that is not `-1`, `0` or in the future. |
| -32001 | invalid phone number | `link.pairCode` or `contacts.lookup` got a number that is too short or not international. |
| -32002 | pair code failed | WhatsApp did not give a Pair Code. |
| -32003 | chat store failed | `chats.list`, `contacts.search`, `chat.start`, `chat.messages`, `chat.send`, `chat.retry`, `chat.pin`, `chat.mute`, `chat.markUnread` or `chat.deleteForMe` could not use the chat store. |
| -32004 | invalid setting | `settings.set` got a value that is not allowed, or the settings file could not be read or written. |
| -32006 | invalid message | `chat.send` or `chat.sendMedia` got a blank or too long text or an unknown Chat. `chat.retry` found no failed Message. `chat.edit` or `chat.revoke` cannot change that Message. `chat.deleteForMe` got an unknown Message. `chat.react` got an unknown or revoked Message or an emoji that is not allowed. `chat.send` got a `quote` that is unknown or revoked. |
| -32007 | could not reach WhatsApp (was "send failed") | `chat.edit`, `chat.revoke`, `chat.react`, `contacts.lookup`, `chat.pin`, `chat.mute`, `chat.markUnread` or `chat.deleteForMe` got no answer from WhatsApp: no connection or a timeout. Nothing changed. |
| -32008 | invalid media | `media.download`, `media.open`, `media.save`, `media.played` got Media that does not exist or is in the wrong state, or a relative `path`. |
| -32009 | media failed | `media.open` or `media.save` failed the integrity check, or could not write the copy. |
| -32010 | invalid chat | `chat.start` got a `jid` that is not a person's WhatsApp ID. `chat.pin`, `chat.mute`, `chat.markUnread` or `chat.deleteForMe` got an unknown Chat. |
| -32011 | pin limit | `chat.pin` would make a 4th Pinned Chat. Nothing changed. |
| -32012 | paste one file at a time | `draft.paste` found two or more copied files. |
| -32013 | file not sendable | `draft.paste` found a copied file that is not a regular file, is empty, or is over 100 MiB, or an image over 100 MiB. |

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
| `pinned` | `pins` | The Go test checks the pin writes that the fake port accepted so far, in order, as `"chat:true"` or `"chat:false"`. The fake port fails them with the `sendError`. |
| `muted` | `mutes` | The Go test checks the mute writes that the fake port accepted so far, in order, as `"chat:until"`. The fake port fails them with the `sendError`. |
| `pictured` | `requests` | The Go test checks the picture calls that reached the fake port so far, in order, as `"chat:knownID"`. A call that the cache answered is not in the list. |
| `revoked` | `ids` | The Go test checks the Message IDs that the fake port has revoked so far, in order. |
| `uploaded` | `uploads` | The Go test checks the Media that the fake port has uploaded and sent so far, in order, as `"id\|kind\|mime\|name\|caption\|content"`. |
| `clock` | `now` | With `now` (Unix seconds), the Go test sets the Daemon time and fires the oldest timer of the Daemon clock. That is the sweep of Disappearing Messages, then a Mute end, in the order the Daemon set them. Without it, the line fires the keyring timeout as above. |
| `restart` | `now` (optional) | The Go test stops the Daemon like a closed stdin and starts it again on the same Profile, with a new fake WhatsApp port. `now` sets the Daemon time while it was off. Put the `whatsapp` start-up lines after it. |
| `file` | `path`, and `content` or `missing` | With `write`: true the Go test writes the file with `content` (a source file for `chat.sendMedia`). With `remove`: true the Go test deletes the file. Otherwise the Go test checks a file: it holds `content`, or (`missing`: true) it does not exist, or (`present`: true) it exists. `$PROFILE` in `path` is the Profile folder. `$RUNTIME` in `path` (and in `in` and `out` frames) is the runtime folder of the test. The Plugin test ignores it. |
| `plugin` | `openChat` | The Plugin test opens this Chat (`jid`, `name`, `kind`) as the user does. The Go test ignores it. |
| `whatsapp` | one of the keys below | The Go test scripts the fake WhatsApp port. |

Keys of a `whatsapp` line. Each line has exactly one:

| Key | Meaning |
|---|---|
| `linked` | `true` or `false`: the Profile is linked at start-up. Put the line before the first `out`. |
| `pairCode` | The code that the next `link.pairCode` returns. |
| `pairError` | `invalid-phone`, or any text: the next `link.pairCode` fails. |
| `sendError` | `offline`, any other text, or empty: the fake port fails every send, edit, revoke, pin and mute with that error, or works again (empty). |
| `lookups` | An object of digits to WhatsApp ID: the answers of `contacts.lookup`. A number that is not in it is not on WhatsApp. `sendError` makes the lookup fail. |
| `pictures` | An object of Chat to `"id\|content"`: the Profile pictures that `chat.picture` finds. A Chat that is not in it has no picture. `sendError` makes the call fail. |
| `phones` | An object of LID to phone JID: what the local LID store holds. The Daemon reads it on `connected`. |
| `hold` | `true` makes the fake port hold every `chat.send` until a `hold` line with `false`. A fixture uses it to end a send after other events. |
| `download` | The plain Media that the next downloads write. |
| `downloadError` | Any text, or empty: the fake port fails every download with that error, or works again (empty). |
| `readError` | `offline`, or empty: the fake port fails every Read Receipt, or works again (empty). |
| `event` | The fake port plays this event now (also `presence` and `chatPresence`, with the JSON keys of their type). `kind` is `pictureChanged` (with `jid`), `qr` (with `code`), `pairSuccess`, `linkFailed` (with `reason`), `loggedOut`, `connected` or `disconnected`. |

The `event` kinds `historySync`, `pushName`, `contactName`, `pin`, `archive`, `unarchiveChats`, `mute` and `readMarker` carry the JSON keys of their type in `daemon/internal/whatsapp/whatsapp.go`. Keys that a fixture leaves out take their zero value. `mutedUntil` and `until` are Unix seconds: 0 is not muted and -1 is muted for ever.

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
