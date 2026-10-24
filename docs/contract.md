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

If the store key was lost, the Daemon deletes the unreadable Profile first. `ready` then follows as for a new Profile. Fixture: `contract/fixtures/lost-key.jsonl`.

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

## Methods (Plugin to Daemon)

### `chats.list`

Params, both optional: `offset` (default 0) and `limit` (default 50, at most 200). Valid once the Daemon is `ready`. The result is one page of the Chat list:

```json
{"chats":[{"jid":"1111@s.whatsapp.net","kind":"direct","name":"Ann","lastActive":100,"pinned":false,"muted":false,"unread":false}],"more":false,"offset":0}
```

- Order: Pinned Chats first, then newest `lastActive` first (Unix seconds). A Chat with no activity is not listed.
- `kind` is `direct`, `group` or `self`.
- `muted` is true for a Muted Chat whose mute has not ended. `unread` is the Read Marker. `pinned`, `muted` and `unread` come from App State.
- `name` follows the name rule below. `more` is true when another page follows at `offset + limit`.
- Error -32003 means the chat store failed.

Name rule, resolved by the Daemon at list time:

1. A Group Chat shows its subject.
2. Any other Chat shows the name that the phone contacts hold for the Participant.
3. Otherwise it shows the Push Name.
4. Otherwise it shows the phone number with a leading `+`.

Fixtures: `chats-list.jsonl`, `chats-names.jsonl` (each step of the rule), `chats-pages.jsonl`.

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

No params. Valid only once the Daemon is `ready` and the Profile is linked. The Daemon asks WhatsApp to remove the Companion Device, so the phone no longer lists it. It then answers with an empty `result` and runs the delete-the-Profile routine (`Profile.Delete`). `unlinked` with `by: "user"` and `ready` follow, then linking starts. Profile Settings stay. If WhatsApp cannot be reached, the Daemon gives error -32005 and keeps the Profile. The Plugin asks the user to confirm before it sends this request. Fixture: `logout.jsonl`.

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
| -32003 | chat store failed | `chats.list` could not read the chat store. |
| -32004 | invalid setting | `settings.set` got a value that is not allowed, or the settings file could not be read or written. |
| -32005 | log out failed | `profile.logout` could not reach WhatsApp. The Profile is unchanged. |

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
| `clock` | `advance` | The Go test fires the oldest keyring timeout (30 s). |
| `whatsapp` | one of the keys below | The Go test scripts the fake WhatsApp port. |

Keys of a `whatsapp` line. Each line has exactly one:

| Key | Meaning |
|---|---|
| `linked` | `true` or `false`: the Profile is linked at start-up. Put the line before the first `out`. |
| `pairCode` | The code that the next `link.pairCode` returns. |
| `pairError` | `invalid-phone`, or any text: the next `link.pairCode` fails. |
| `event` | The fake port plays this event now. `kind` is `qr` (with `code`), `pairSuccess`, `linkFailed` (with `reason`) or `loggedOut`. |

The `event` kinds `historySync`, `pushName`, `contactName`, `pin`, `mute` and `readMarker` carry the JSON keys of their type in `daemon/internal/whatsapp/whatsapp.go`. Keys that a fixture leaves out take their zero value. `mutedUntil` and `until` are Unix seconds: 0 is not muted and -1 is muted for ever.

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
