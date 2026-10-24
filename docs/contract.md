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
| `unlinked` | none | The phone removed the Companion Device. The Daemon has deleted the Profile and its store key. `ready` follows, then linking starts as on a fresh Profile. |

Reasons of `linkFailed`. The Go source of truth is `daemon/internal/whatsapp/whatsapp.go`.

| Reason | Meaning |
|---|---|
| `timeout` | The user did not scan the last QR code in time. |
| `client-outdated` | WhatsApp rejects this whatsmeow version. |
| `scanned-without-multidevice` | The phone scanned the code but multi-device is off. |
| `pair-error` | The phone authorized the link, but pairing failed. |
| `connect-failed` | The Daemon could not connect to WhatsApp. |
| `unexpected` | Any other linking failure. |

Fixtures: `contract/fixtures/link-qr.jsonl`, `link-failed.jsonl`, `link-unlinked.jsonl`.

## Methods (Plugin to Daemon)

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

Fixture: `contract/fixtures/unknown-method.jsonl`.

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
