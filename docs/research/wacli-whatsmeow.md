# wacli and whisker on whatsmeow

Scope: how [wacli](https://github.com/openclaw/wacli) uses `go.mau.fi/whatsmeow`, how whisker's Daemon differs, and what whisker can adopt.
Research date: 2026-10-30. Source only. No wacli code ran.

Sources:
- wacli at commit `bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5` (links below use `W/` for `https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/`).
- whatsmeow at `v0.0.0-20261017175727-b2b1e71d45e2`, read from the Go module cache (links use `M/` for `https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/`).
- whisker paths are relative to the repo root.

## Answer

Both projects pin the same whatsmeow commit (`daemon/go.mod` line 12, wacli `go.mod`). So every library claim below applies to both.

wacli is a CLI. Each command opens the stores, connects, works, and exits. A `sync --follow` mode keeps one connection open.
whisker is a long-running Daemon behind a Plugin UI. It restarts with every shell restart.
Many wacli features exist because of the CLI model: idle exit, stale-keepalive reconnect, a detached dial context, manual History Sync download, multi-account config. They do not fit whisker.

whisker is already stricter or simpler in these areas:
- Encrypted stores (ncruces + adiantum) instead of plain SQLite with mode `0600`.
- No whatsmeow or libsignal log output at all. wacli prints whatsmeow ERROR lines, which hold JIDs.
- A pre-generated Message ID for each send, so a retry cannot make a duplicate.
- One port interface (`whatsapp.Port`) with one Fake, so tests never touch whatsmeow.

wacli uses five whatsmeow features that whisker does not use, and they close real gaps in whisker:
1. `UseRetryMessageStore`: answer retry receipts after a restart.
2. `AutomaticMessageRerequestFromPhone`: ask the phone for a Message that does not decrypt.
3. `SendMediaRetryReceipt`: ask the phone to re-upload expired Media.
4. A presence resend on `events.PushNameSetting`: the first presence after a new link fails without a push name.
5. `Width`, `Height` (and `JPEGThumbnail`) on sent images.

See the ranked list in "Recommendations".

## 1. Client setup

**wacli.**
- Opens `sqlstore.New(ctx, "sqlite3", FileURI(StorePath, "_foreign_keys=on"), dbLog)` with `mattn/go-sqlite3` (cgo) ([W/internal/wa/session.go#L17-L46](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/session.go#L17-L46)).
- It runs `chmod 0600` on the store files before and after the open (L23, L35).
- It uses `GetFirstDevice`, and `NewDevice` on `sql.ErrNoRows` (L39-L46).
- It sets `EmitAppStateEventsOnFullSync`, `UseRetryMessageStore` and `AutomaticMessageRerequestFromPhone` to true (L56, L62, L84).
- It wraps the contact store in a journal to record app-state writes in order (L48-L81, L92-L105).
- Logging: a custom `waLog.Logger` at level ERROR to stderr for both the client and the DB ([W/internal/wa/logger.go#L88-L140](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/logger.go#L88-L140)).
- libsignal gets a logger that keeps only known operation labels and redacts the rest (L17-L85 of the same file).
- Device identity: `store.SetOSInfo(label, {0,1,0})`, `DeviceProps.PlatformType` from `WACLI_DEVICE_PLATFORM` (default DESKTOP), and `BaseClientPayload.UserAgent.Device/Manufacturer` set to the label ([W/cmd/wacli/main.go#L21-L39](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/main.go#L21-L39)).
- History window: `SetHistorySyncLimits` writes `DeviceProps.HistorySyncConfig` and `RequireFullSync` before pairing ([W/internal/wa/client.go#L134-L179](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/client.go#L134-L179)).

**whisker.**
- `internal/profile/profile.go:113` opens `sqlstore.New(..., "sqlite3", dsn(device.db, hexKey), waLog.Noop)` on the encrypted ncruces driver.
- `internal/whatsapp/client.go:79-90` uses `GetFirstDevice` and `whatsmeow.NewClient(dev, waLog.Noop)`. It sets only `EmitAppStateEventsOnFullSync = true`.
- `internal/whatsapp/client.go:62-68` sets `SetOSInfo("whisker (host)", version)` and `PlatformType = DESKTOP`.
- `internal/whatsapp/signallog.go:5-18` drops every libsignal line. `cmd/whisker-daemon/stdout.go:11-23` points fd 1 at stderr, so no library can write a frame.

**Difference.** whisker has no whatsmeow log at all. wacli logs ERROR lines.
whatsmeow's error lines carry JIDs and raw nodes (for example `"Temporary ban connect failure: %s", node`, [M/connectionevents.go#L134](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/connectionevents.go#L134)). That breaks whisker's log rule (`docs/architecture.md` line 90: ids and error codes only).
The two retry flags are the real gap (see sections 3 and 6).

**Should whisker change?** Yes for `UseRetryMessageStore` and `AutomaticMessageRerequestFromPhone`. No for the logger and the history limits.

## 2. Pairing, logout, profiles, locking

**wacli.**
- `Connect` gets the QR channel, dials, then loops on QR items. With a phone number it calls `PairPhone(ctx, phone, true, PairClientChrome, "Chrome (Linux)")` on the first QR code event ([W/internal/wa/client.go#L199-L311](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/client.go#L199-L311)).
- It maps every QR channel result to an error, including passkey request and passkey confirmation (L313-L335).
- Multi-account: named accounts, each with its own `store_dir` ([W/cmd/wacli/accounts.go#L22](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/accounts.go#L22)). Each dir holds `session.db` (whatsmeow) and `wacli.db` (wacli) ([W/internal/app/app.go#L172](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/app.go#L172), L205).
- Locking: `flock(LOCK_EX|LOCK_NB)` on `<store_dir>/LOCK` ([W/internal/lock/lock_unix.go#L12](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/lock/lock_unix.go#L12)).
- On `LoggedOut`, `sync` stops and keeps the local data ([W/internal/app/sync_events.go#L247-L262](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync_events.go#L247-L262)).

**whisker.**
- `internal/whatsapp/client.go:171-212`: `Connect` gets the QR channel, dials, and forwards codes as events. `PairCode` (L214-222) uses the same `PairPhone` arguments.
- `qrFailureReason` (L202-212) maps timeout, client outdated and no multidevice. Every other item, passkey included, becomes `ReasonUnexpected`.
- One Profile. `profilelock.Acquire` in `$XDG_RUNTIME_DIR/whisker` (`cmd/whisker-daemon/main.go:63`).
- On `LoggedOut` the Daemon deletes the Profile and links again (`internal/contract/session.go`, `unlink`).

**Difference.** Same pairing calls. wacli names the passkey case. whisker shows "unexpected".

**Should whisker change?** No for multi-account (YAGNI). A distinct passkey reason is optional and small (item 7).

## 3. Events, reconnect, session end

**wacli.**
- One handler per run with a big type switch ([W/internal/app/sync_events.go#L65-L262](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync_events.go#L65-L262)). It has its own `recover` (L79). Work runs on whatsmeow's dispatch goroutine.
- It handles `UndecryptableMessage` and emits a warning with the fail mode (L169-L187, L960-L981).
- It logs `OfflineSyncPreview` and `OfflineSyncCompleted` (L188-L206).
- On `Connected` it clears the group-info cache and sends available presence (L207-L219).
- On `PushNameSetting` it sends available presence again (L222-L227).
- On `StreamReplaced` it calls `Disconnect` (L234-L242).
- Reconnect: in follow mode with a stale threshold, wacli turns off whatsmeow's auto-reconnect and runs its own `ReconnectWithBackoff` ([W/internal/wa/client.go#L566-L587](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/client.go#L566-L587), [W/internal/app/sync.go#L125-L135](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync.go#L125-L135)).

**whisker.**
- `internal/whatsapp/events.go:19-152`: one handler maps whatsmeow events to port events on a 64-slot channel. The Daemon loop reads them (`internal/contract/session.go`, `onEvent`).
- `StreamReplaced`, `TemporaryBan` and `ConnectFailure` become `Disconnected{Reason}` (L46-51). The ban code and expiry are dropped.
- No case for `UndecryptableMessage`, `PushNameSetting`, `KeepAliveTimeout` or offline sync.
- Reconnect after a drop: whatsmeow's default `EnableAutoReconnect: true` ([M/client.go#L305](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/client.go#L305)) with linear backoff of 2 s per error ([M/client.go#L644-L651](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/client.go#L644-L651)). The first connect retries every 5 s (`internal/contract/outbox.go:20`, `session.go:143-152`).

**whatsmeow facts that matter here.**
- `dispatchEvent` already recovers a handler panic and logs it ([M/client.go#L995-L1010](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/client.go#L995-L1010)). With `waLog.Noop`, whisker loses that log line. The process stays up.
- `StreamReplaced` and an unknown `ConnectFailure` call `expectDisconnect`, so there is no auto-reconnect ([M/connectionevents.go#L49](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/connectionevents.go#L49), [L102-L157](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/connectionevents.go#L102-L157)). wacli's extra `Disconnect` is not needed in whisker.
- Keepalive failures force a reconnect after `KeepAliveMaxFailTime` = 3 min ([M/keepalive.go#L27](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/keepalive.go#L27), L46).
- `SendPresence` fails with `ErrNoPushName` while `Store.PushName` is empty ([M/presence.go#L60-L69](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/presence.go#L60-L69)). whatsmeow fills `Store.PushName` from app state and then dispatches `PushNameSetting` ([M/appstate.go#L365-L370](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/appstate.go#L365-L370)).

**Difference.** whisker's presenter sends presence once per connection (`internal/contract/presence.go:38-58`). On a failure it logs "presence failed" and does not try again until attention or a setting changes.
After a fresh link the push name can arrive after `Connected`. Then the first `SendPresence` fails, and other users can see "-" as the name (whatsmeow doc comment, presence.go L62-63). wacli covers this with the `PushNameSetting` case.
whisker also drops `UndecryptableMessage`, so a Message that does not decrypt leaves a silent hole.

**Should whisker change?** Yes: route `PushNameSetting` to `presenter.sync()` (item 4). Yes: enable the phone re-request (item 2). The reconnect, keepalive and StreamReplaced handling can stay.

## 4. History Sync and App State

**wacli.**
- During `sync` it sets `ManualHistorySyncDownload = true` ([W/internal/app/sync.go#L136-L137](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync.go#L136-L137)). It downloads the blob, handles it, then calls `DeleteMedia` ([W/internal/app/sync_events.go#L676-L694](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync_events.go#L676-L694)).
- On-demand history: `BuildHistorySyncRequest` sent with `SendMessage(ownID, ..., Peer: true)` ([W/internal/wa/client.go#L418-L446](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/client.go#L418-L446)).
- App state: a large recovery system with `BuildAppStateRecoveryRequest`, `FetchAppState` through `DangerousInternals`, and tables for recovery intents and missing keys (client.go L448-L499; [W/internal/store/migrations.go#L206](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/store/migrations.go#L206), L284, L296, L1091).

**whisker.**
- Automatic download (whatsmeow default). whatsmeow then downloads, dispatches `events.HistorySync` and calls `DeleteMedia` itself ([M/message.go#L712-L740](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/message.go#L712-L740)).
- `internal/whatsapp/events.go:470-527` maps conversations, push names, reactions and LID aliases.
- On-demand: `SendPeerMessage(BuildHistorySyncRequest(...))` (`internal/whatsapp/send.go:154-165`).
- App state: Pin, Archive, Mute, Read Marker, Delete, Clear and Contact events. Full-sync flags wait for `AppStateSyncComplete` and go out as one batch (`events.go:131-150`, L240-261).

**Difference.** The download path ends in the same calls. wacli needs the manual mode because a CLI run must know when the work is done. whisker does not.
wacli's app-state recovery solves stuck collections. whisker has no report of such a failure, so this is not confirmed as a gap.

**Should whisker change?** No.

## 5. Message storage and search

**wacli.**
- Own DB `wacli.db`, separate from `session.db`. The `messages` table holds text, media keys, `local_path`, `media_unavailable_at`, `revoked`, `edited` and `UNIQUE(chat_jid, msg_id)` ([W/internal/store/schema.sql#L58-L95](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/store/schema.sql#L58-L95)).
- FTS5 table `messages_fts`, with a `LIKE` fallback ([W/internal/store/migrations.go#L936-L947](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/store/migrations.go#L936-L947)).
- LID rows: canonical JID is the phone JID through `ResolveLIDToPN` ([W/internal/app/jid.go#L20-L22](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/jid.go#L20-L22)). A migration rewrites old LID rows ([W/internal/app/lid_migration.go](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/lid_migration.go)).

**whisker.**
- Own encrypted `chat.db` (`internal/profile/profile.go:118`). `PutLive` drops a second copy with a known ID (`internal/chatstore/messages.go:709-757`).
- Search runs in Go. FTS5 is out by design (`docs/architecture.md` line 182).
- LID handling: phone JID as Chat key, with alias merges (`internal/whatsapp/client.go:428-526`, `events.go:392-467`).

**Difference.** Same model: own store, phone JID as key. wacli adds FTS5.

**Should whisker change?** No.

## 6. Sending

**wacli.**
- `SendMessage` with no fixed ID. whatsmeow makes the ID ([W/internal/wa/send.go#L17-L57](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/send.go#L17-L57)).
- Images carry `Width`, `Height` and a `JPEGThumbnail` ([W/cmd/wacli/send_file.go#L338-L366](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/send_file.go#L338-L366)).
- Outgoing mentions set `ContextInfo.MentionedJID` ([W/cmd/wacli/send.go#L550](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/send.go#L550)).
- After a send, the CLI stays connected for a while so it can answer retry receipts (send.go L54, L146, L171). With `UseRetryMessageStore` a later run can also answer them.

**whisker.**
- The Daemon makes the ID first (`NewMessageID`, `send.go:19`) and passes it in `SendRequestExtra{ID}` (L21-38, L150). A retry reuses it.
- Quotes use the LID of the author in LID groups (`wireJID`, L76-93).
- `SendMedia` (L110-148) sets URL, keys, length, MIME and caption. It sets no `Width`, `Height` or `JPEGThumbnail`.
- No `UseRetryMessageStore` and no `GetMessageForRetry`. whatsmeow keeps only 256 recent sent Messages in memory ([M/retry.go#L36](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/retry.go#L36)).

**whatsmeow facts.**
- With `UseRetryMessageStore`, whatsmeow writes the plaintext proto of each sent Message to `whatsmeow_retry_buffer` ([M/retry.go#L52-L76](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/retry.go#L52-L76)). It reads it back for a retry receipt (L124-L129).
- It deletes rows older than 7 days, at most every 12 h, and only on a later send ([M/store/sqlstore/store.go#L1118-L1126](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/store/sqlstore/store.go#L1118-L1126), retry.go L72-L77).

**Difference.**
- Retry receipts: the Daemon restarts with the shell. A recipient device that asks for a retry after a restart gets no answer. It then shows "Waiting for this message". wacli's comment says the same (session.go L57-L61).
- `GetMessageForRetry` does not fit whisker. The chat store keeps text but not the full proto, and whisker discards the upload key of sent Media (`docs/architecture.md` line 83). So whisker cannot rebuild a Media Message.
- Image fields: without `Width` and `Height`, other clients do not know the aspect ratio before download. Without `JPEGThumbnail` they show no preview. Exact effect on each official client: not confirmed.

**Should whisker change?** Yes: `UseRetryMessageStore` (item 1). Yes: image dimensions (item 5).

## 7. Media download and expired Media

**wacli.**
- Lazy by default. Download jobs call `DownloadMediaToFile` ([W/internal/app/media.go#L227-L250](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/media.go#L227-L250)).
- On HTTP 403, 404 or 410 it treats the Media as expired ([W/internal/app/media_retry.go#L443-L449](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/media_retry.go#L443-L449)).
- It then sends `SendMediaRetryReceipt` and waits for `events.MediaRetry` (media_retry.go L172-L188, L289). It decrypts the answer with `DecryptMediaRetryNotification` and maps "not on phone" to `NotFound` ([W/internal/wa/mediaretry.go#L56-L67](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/mediaretry.go#L56-L67)).
- It downloads from the new direct path, or marks the row `media_unavailable_at` (media_retry.go L410).
- For a chat with LID addressing it rewrites the chat and sender to LIDs before the receipt ([W/internal/wa/mediaretry.go#L24-L51](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/mediaretry.go#L24-L51)). For groups it reads the addressing mode from `GetGroupInfo` (media_retry.go L236-L256).
- wacli downloads the retried file with its own downloader and no ciphertext hash ([W/internal/wa/media.go#L128-L136](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/media.go#L128-L136)). Its comment says the re-uploaded ciphertext can differ. Not confirmed necessary: whatsmeow's own doc swaps only `DirectPath` and downloads with the original fields ([M/mediaretry.go#L60-L76](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/mediaretry.go#L60-L76)).

**whisker.**
- Lazy, with auto-download by size policy (`internal/contract/media.go:50-110`). `Download` calls `DownloadMediaWithPathToFile` into a capped file (`internal/whatsapp/client.go:409-413`).
- `mediaLoader.done` turns every error into `MediaFailed` (`internal/contract/media.go:144-164`). The retry link runs the same download with the same direct path.
- whatsmeow returns `ErrMediaDownloadFailedWith403/404/410` at once on those statuses, without trying the other hosts ([M/download-to-file.go#L104-L116](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/download-to-file.go#L104-L116)).

**Difference.** CDN links expire. Media from History Sync is often old, so many of those downloads fail with 404 or 410. In whisker, Retry then fails the same way every time.
wacli asks the phone to upload again, and whisker does not.

**Should whisker change?** Yes (item 3). The size is M:
- a port method `RequestMediaRetry` and a port event for `events.MediaRetry`, plus the Fake;
- a stored new `DirectPath`;
- the LID sender for LID groups (whisker already has the addressing map in `lidGroups`, `send.go:53-73`);
- a distinct "not on phone" end state, so the retry link stops.

## 8. Groups, LID, newsletters, status

**wacli.** Caches group info and forgets it on `GroupInfo`, `JoinedGroup` and `Connected` ([W/internal/app/group_info.go#L44](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/group_info.go#L44), L142; sync_events.go L164-L168, L207-L210). It has commands for channels (newsletters), status Messages, polls and calls ([W/cmd/wacli/channels.go](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/channels.go), [W/internal/store/migrations.go#L798-L858](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/store/migrations.go#L798-L858)).

**whisker.** Remembers only the addressing mode of each group (`lidGroups`, `client.go:55-57`, `send.go:53-73`). It calls `GetGroupInfo` for participants and admin checks (`client.go:106-162`). whatsmeow keeps its own group cache for sends. Broadcast and status Messages are dropped (`events.go:70`).

**Difference.** wacli covers more WhatsApp features. The feature set of whisker is a product choice and is out of scope here.

**Should whisker change?** No.

## 9. Errors, tests, seams

**wacli.** An `App` with a WhatsApp interface (`internal/app/app.go` L26-L112) and a test fake (`internal/app/fake_wa_test.go`). 204 test files.

**whisker.** `whatsapp.Port` (`internal/whatsapp/whatsapp.go:16`) and `whatsapp.Fake` (`fake.go`). The real `Client` never runs in tests. A `groupInfo` func field is a seam for the group lookup (`client.go:57`). 61 test files.
Errors: the Daemon logs fixed strings such as "media download failed" and drops the error text, because it can hold hosts (`internal/contract/media.go:151`).

**Difference.** The same seam shape. whisker's logs lose the reason code too: a 410, a timeout and a bad HMAC all print the same line.

**Should whisker change?** A status code or a whatsmeow error class in the log line is safe under the log rule and helps debugging (item 6).

## Not recommended

- **whatsmeow logger at ERROR.** Its lines hold JIDs, URLs and raw nodes. whisker's `waLog.Noop` and silent libsignal logger are stricter on purpose.
- **Own reconnect backoff and stale-keepalive reconnect.** whatsmeow already reconnects with backoff and forces a reconnect after 3 min of failed keepalives.
- **`Disconnect` on `StreamReplaced`.** whatsmeow already expects the disconnect, and whisker does not reconnect.
- **Manual History Sync download and `HistorySyncLimits`.** The automatic path does the same work. No spec asks for a larger window.
- **App-state recovery system.** It is large (L). Not confirmed as a whisker problem.
- **FTS5, multi-account, plain mattn SQLite.** Ruled out by `docs/architecture.md` or not in scope.
- **`SynchronousAck` and `EnableDecryptedEventBuffer`.** Neither project uses them. A loss window when the Daemon stops with events still in the 64-slot channel is not confirmed.
- **Notifications for the offline backlog.** whisker notifies for each new live Message (`internal/notifypolicy`). A burst after a long offline period is possible. `OfflineSyncPreview` and `OfflineSyncCompleted` could bound it, but no report shows the problem. Not confirmed.

## Recommendations

Ranked by value for size. Each item solves a gap that the code shows.

1. **Set `UseRetryMessageStore = true`** in `NewClient` (`internal/whatsapp/client.go:84-88`).
   - Why: the Daemon restarts with the shell. After a restart it cannot answer retry receipts, and recipients see "Waiting for this message". wacli does this ([session.go#L57-L62](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/session.go#L57-L62)). whatsmeow keeps only 256 in memory ([retry.go#L36](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/retry.go#L36)).
   - Size: S (one line).
   - Risk: whatsmeow keeps the plaintext proto of each sent Message in `device.db` for up to 7 days ([store.go#L1118-L1126](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/store/sqlstore/store.go#L1118-L1126)). That includes revoked and Disappearing Messages. `sweepExpired` and Revoke do not reach that table. The file is encrypted at rest. Record this in `docs/architecture.md` section 5 before you ship.

2. **Set `AutomaticMessageRerequestFromPhone = true`** in the same place.
   - Why: without it, whatsmeow asks only the sender for a resend. With it, whatsmeow also asks the phone after 5 s ([retry.go#L420-L452](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/retry.go#L420-L452), L493-L498). The answer comes back as a normal `*events.Message` ([message.go#L863-L884](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/message.go#L863-L884)). whisker already handles that event, and `PutLive` drops a duplicate ID. wacli does this ([session.go#L82-L84](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/session.go#L82-L84)).
   - Size: S (one line).
   - Risk: low. The recovered Message arrives late and can raise a notification then.

3. **Ask the phone to re-upload expired Media.**
   - Why: `mediaLoader.done` makes every failure `MediaFailed`, and Retry repeats the same dead link (`internal/contract/media.go:144-164`). History Sync Media is often expired. wacli recovers it with `SendMediaRetryReceipt` ([media_retry.go#L172-L188](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/media_retry.go#L172-L188), [L289](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/media_retry.go#L289)).
   - Shape: on a 403, 404 or 410, send the receipt. On `events.MediaRetry`, decrypt it, store the new `DirectPath`, and download again with the original hashes (whatsmeow's doc pattern, [mediaretry.go#L60-L76](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/mediaretry.go#L60-L76)). On "not on phone", store an end state so the retry link stops. Use the LID sender in LID groups.
   - Size: M (port method, port event, Fake, one chat store column, a contract test).
   - Risk: medium. It needs the phone online. The LID rewrite must match the addressing of the group, as wacli does ([mediaretry.go#L38-L51](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/mediaretry.go#L38-L51)).

4. **Resend presence on `events.PushNameSetting`.**
   - Why: `SendPresence` fails with `ErrNoPushName` until whatsmeow has the push name ([presence.go#L60-L69](https://github.com/tulir/whatsmeow/blob/b2b1e71d45e2/presence.go#L60-L69)). After a fresh link it can come after `Connected`. whisker's presenter then does not try again until attention changes (`internal/contract/presence.go:45-58`). wacli resends on this event ([sync_events.go#L222-L227](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/app/sync_events.go#L222-L227)).
   - Shape: map the event to a port event and call `presenter.sync()` with `known = false`.
   - Size: S.
   - Risk: low.

5. **Send `Width` and `Height` with each image; `JPEGThumbnail` is optional.**
   - Why: `SendMedia` sets neither (`internal/whatsapp/send.go:122-126`). wacli sets both and a thumbnail ([send_file.go#L338-L366](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/cmd/wacli/send_file.go#L338-L366)).
   - Shape: `image.DecodeConfig` for JPEG and PNG. Read WebP dimensions with the parser in `internal/media/strip.go`, with no new dependency. Swap width and height when the kept EXIF Orientation is 5-8.
   - Size: S for dimensions. M with a thumbnail (decode, scale, encode).
   - Risk: low. The effect on each official client is not confirmed. Check it on a phone first.

6. **Log a reason code, not only a fixed string.**
   - Why: "media download failed", "connect failed" and "presence failed" hide the cause (`internal/contract/media.go:151`, `session.go:145`, `presence.go:53`). An HTTP status, `ConnectFailure` reason or `TemporaryBan` code holds no text, host or name, so the log rule allows it (`docs/architecture.md` line 90).
   - Size: S.
   - Risk: low. Log only typed codes, never `err.Error()`.

7. **Name the passkey pairing case.** Map `QRChannelEventPasskeyRequest` and `QRChannelEventPasskeyResponse` to their own `LinkFailed` reason (`internal/whatsapp/client.go:202-212`). wacli does this ([client.go#L323-L326](https://github.com/openclaw/wacli/blob/bf7f72e7b30c372e07c2a9ff9acceb087e5e49d5/internal/wa/client.go#L323-L326)).
   - Size: S (plus one Plugin string).
   - Risk: low. Do it only when a user meets it: YAGNI.
