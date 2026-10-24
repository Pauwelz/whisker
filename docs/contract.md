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

## Methods (Plugin to Daemon)

### `keyring.retry`

Valid only after `keyringUnavailable`. The Daemon answers with an empty `result` at once. Then it joins the keyring attempt that is still in flight (or starts one) and sends `ready` or `keyringUnavailable` again. Any other time, the method does not exist.

## Errors

The Daemon answers a request with an `error` object. Application codes will join the standard ones here.

| Code | Message | When |
|---|---|---|
| -32700 | parse error | The line is not a JSON-RPC object. The `id` is `null`. |
| -32601 | method not found | The method does not exist. |

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

- The Go test (Seam A) runs the lines in order: it sends each `in` line and waits for each `out` frame. At the end it checks that no frame is left. It ignores `view` lines.
- The `node --test` test (Seam B) folds the `out` frames through `WhiskerState.js` and checks each `view` line. It ignores `in` lines.
- Later tickets add line kinds (fake WhatsApp events, clock steps) here, in this table.

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
