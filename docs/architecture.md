# whisker architecture

## 1. Purpose and scope

whisker is the Omarchy product: a Quickshell/QML Plugin and its own Go Daemon. It runs only on Linux. It is built from the ground up and shares no code with other products. [ADR 0007](https://github.com/Pauwelz/whisker-legacy/blob/main/docs/adr/0007-split-into-whisker-and-catty.md) records the split. This spec names the language, the process model, the modules, the storage and the distribution, so an implementer can start the repo from it. It does not list the v1 features or the CI setup. Terms such as Profile, Companion Device, Daemon, Plugin and Media follow the [glossary](../GLOSSARY.md).

## 2. Language and key dependencies

- The Plugin is QML on Quickshell. It uses `Quickshell.Io` to start and talk to the Daemon.
- The Daemon is Go. It is one static binary, built with `CGO_ENABLED=0`. There is no bridge process and no protobuf layer.
- `whatsmeow` handles the WhatsApp protocol and its `sqlstore`.
- `ncruces/go-sqlite3` with the `adiantum` VFS gives encrypted SQLite in pure Go.
- `zalando/go-keyring` reads and writes the store key in the Secret Service.
- `godbus/dbus` calls the Notifications service on the session bus.
- `golang.org/x/sys/unix` gives `prctl` (`PR_SET_DUMPABLE` off, so no core dumps) and `openat2` (opens attachments without following links).
- `go.mau.fi/libsignal` is a direct dependency only to replace its logger with a silent one. Its log lines hold raw bytes of remote Messages.
- `rsc.io/qr` renders the QR code ([research](research/qr-library.md)).
- The Daemon uses whatsmeow's `util/hkdfutil` and `util/cbcutil` packages for Media.
- The JSON-RPC 2.0 envelope uses no library on either side.
- Release tooling: POSIX `sh`, `curl`, `sha256sum` and a GitHub Actions workflow (see section 8).

## 3. Process model

- Quickshell starts the Daemon as a child process. The Daemon stops with the Plugin.
- The Plugin talks to the Daemon over the child's stdio. stdin carries commands. stdout carries protocol frames only. stderr carries logs. There is no socket.
- At start, the Daemon copies fd 1 for the frame encoder and points fd 1 at stderr. A library that prints to stdout cannot inject a frame.
- The Daemon exits on stdin EOF. A hard shell crash leaves no orphan Daemon.
- The Daemon `Process` lives in a Plugin `service` with `keepLoaded: true`, so a config reload keeps it. Quickshell kills the child when it exits. The Plugin does not use `startDetached()` ([research](research/omarchy-plugin.md)).
- One Daemon serves one Profile, which is one Companion Device. The initial architecture has no support for several Profiles.
- The Profile lock in `$XDG_RUNTIME_DIR/whisker/` stops a second Daemon on the same Profile. A new Daemon waits a few seconds for the lock. Then it exits with a "Profile in use" code. This covers the shell restart race.
- To debug, run `whisker-daemon` in a terminal and type JSON lines.

## 4. Modules and seams

```
manifest.json, QML         the Plugin (repo root)
state/*.js                 the Plugin state logic, one module per topic (QML-free)
scripts/fetch-daemon.sh    downloads and verifies the Daemon
bin/                       the Daemon binary (git-ignored)
daemon/
  go.mod
  cmd/whisker-daemon       the entry point
  internal/...             the Daemon packages
docs/contract.md           the Plugin and Daemon contract
contract/fixtures/*.jsonl  golden fixtures
```

- **Plugin (QML).** It shows state, sends commands and owns all presentation. It holds no Profile state.
- **Daemon (Go).** It owns the Profile: the stores, the Media, the session and linking.
- **Seam 1: stdio JSON-RPC.** This is the only runtime seam between the Plugin and the Daemon (section 7). The Go tests and the Plugin's JS tests both read the golden fixtures in `contract/fixtures/*.jsonl`. This keeps both sides in step.
- **Desktop notifications.** The Daemon shows them: it calls `org.freedesktop.Notifications.Notify` on the session bus (`daemon/internal/desktop`). The Plugin takes no part. The port `desktop.Notifier` has a fake, so the contract tests run without a bus. The click hint runs `omarchy-shell pauwelz.whisker openChat <token>`. The Daemon maps the token to the Chat.
- **Seam 2: the Daemon binary.** The Plugin gets the binary through `scripts/fetch-daemon.sh`. It calls the script through `Quickshell.Io` and reads the exit code (section 8).
- **Seam 3: whatsmeow.** The Daemon embeds whatsmeow in-process. whatsmeow's `sqlstore` runs on the encrypted SQLite driver. At runtime whatsmeow packages stay inside `daemon/internal/whatsapp` (the client in `client.go`, sends in `send.go`, events in `events.go`, System Messages in `system.go` and the message content in `content.go`). Two other packages import small parts: `internal/profile` opens the device store (`sqlstore`), and `internal/media` uses the crypto helpers (`cbcutil`, `hkdfutil`). Tests in `daemon/internal/whatsapp` may import whatsmeow to build inputs, such as a History Sync batch.
- **Store.** The Daemon has a device store (whatsmeow's) and a chat store (Chats, messages, Drafts and the FTS index). Both are encrypted SQLite (section 5).

The split of `daemon/internal/...` into packages is not decided (see section 9).

## 5. Profile and storage

### Paths

| Path | Content |
|---|---|
| `$XDG_DATA_HOME/whisker/` | the stores and Media |
| `$XDG_CONFIG_HOME/whisker/` | Profile Settings |
| `$XDG_RUNTIME_DIR/whisker/` | the Profile lock and the plain Media copies (`media/`) |

whisker never reads, detects or deletes the Profile of whisker-legacy in `~/.config/Whisker/`.

### Encryption at rest

The scheme covers the device store, the chat store and the Media. Profile Settings stay plain.

1. **SQLite stores.** `ncruces/go-sqlite3` with the `adiantum` VFS, in pure Go. The main file, the WAL and the journal are encrypted. Both stores use the same key. SQLCipher is out, because it needs cgo.
2. **Known limit.** The encryption gives confidentiality only. It does not detect tampering or rollback.
3. **Key.** The Daemon makes one random 32-byte store key per Profile on first start. It keeps the key in the Secret Service through `go-keyring`, base64-encoded. whisker never writes the key to disk itself. The Secret Service provider decides how it stores the key ([research](research/go-keyring.md)).
4. **No keyring or locked keyring.** The Daemon waits at most about 30 s. `go-keyring` has no timeout, so the Daemon sets its own. Then it sends a "keyring unavailable" state to the Plugin, with a Retry action. There is no plain-file or key-file fallback for the store key. The Daemon reads the default keyring file only to detect a plain one (`keystore.PlainKeyring`, see Threat boundary).
5. **Lost key.** The Daemon renames the unreadable Profile folder to `whisker.unreadable-<time>` and starts linking again. It never deletes a Profile by itself, and a Log out from Settings removes these folders. An unlink from the phone keeps them. `ready` holds `profileMovedAside`, and the Plugin shows a notice. There is no recovery code.
6. **Media at rest.** The Profile keeps Media in the CDN format of WhatsApp: AES-256-CBC with a truncated HMAC-SHA256. The HMAC detects tampering.
   The keys do not come from the `mediaKey` of the Message. The Daemon derives a root key `HKDF(storeKey, "whisker media files")`, as it does for the Media names. Each Message id gets its own key, `HMAC(root, "file\x00" + id)`. No stored file equals its CDN blob.
   - Sent Media: the Daemon encrypts the picked file into the Profile under this key.
     Each upload attempt decrypts a plain copy into `$XDG_RUNTIME_DIR/whisker/media/upload/`. The Daemon removes it when the attempt ends, and wipes the folder at start and at exit.
     whatsmeow encrypts that copy again under a fresh key, which is the key WhatsApp knows. The Daemon does not keep that key.
   - Received Media: whatsmeow downloads the plaintext into `$XDG_RUNTIME_DIR/whisker/media/` with the `mediaKey` of the Message. The Daemon encrypts it again into the Profile with `cbcutil.EncryptStream` under the derived key.
   - Media stored before this change fails the HMAC check. There is no migration. Log out and a new link clear it.
7. **Media on request.** The Daemon copies the file to `$XDG_RUNTIME_DIR/whisker/media/` (tmpfs, mode `0700`). It checks the HMAC and decrypts the copy in place with `cbcutil.DecryptFile`. It gives the path to the Plugin.
   - The Daemon deletes the plain copy when the Chat closes. It wipes the folder at Daemon start, before it needs the store key, and at exit, also when the Profile never opened. A hard kill (SIGKILL) skips the exit wipe, so copies stay until the next Daemon start. The pasted images of Drafts are in a different folder and stay.
   - "Save as" and "open in another app" are explicit exports to a path the user chooses. That copy is plain.
8. **Re-implemented helpers.** whatsmeow has no public function to decrypt a Media file on disk. The Daemon re-implements `getMediaKeys` (HKDF-SHA256 of the per-file key with the Media type as info, 112 bytes: IV, cipher key, MAC key) and `validateMedia` (HMAC over IV and ciphertext, truncated to 10 bytes). This takes about 20 lines. It uses the public `util/hkdfutil.SHA256`. A whatsmeow version bump that changes `util/` breaks the build, so the change is caught at compile time.
9. **Logs.** Logs are plain. They never hold message text, captions, contact names or Media paths. They hold only ids and error codes.

### Sent Messages for retries

A recipient that cannot decrypt a sent Message sends a retry receipt. The Daemon must then send the exact original Message again.

1. **Where.** The chat store keeps each sent Message proto in the table `retry_messages`. whatsmeow's own table `whatsmeow_retry_buffer` stays empty. `UseRetryMessageStore` is on, and the Daemon replaces `Device.EventBuffer` with a wrapper. The wrapper passes the incoming-buffer methods to whatsmeow unchanged. It serves the outgoing methods from the chat store, which is why the table is encrypted like the rest of the chat store.
2. **How long.** 7 days, the limit of whatsmeow. The Daemon removes older rows when whatsmeow asks, about every 12 hours.
3. **Early delete.** The chat store deletes the rows of a Message when it revokes, deletes or expires the Message, or clears or deletes the Chat. A retry receipt then gets no resend. The rows are keyed by Message id, not by Chat: the JID that whatsmeow sent to can be a phone-number JID or a LID. A row of an edit or a reaction stores the id it acts on (`target_id`), so deleting the target removes them too. A revoke stores no target: it holds no text, and it must stay retryable after the revoke deletes its target. There is no foreign key, because those ids are not in `messages`.
4. **Receiving.** `AutomaticMessageRerequestFromPhone` is on. When a Message does not decrypt, the Daemon asks the phone to send it again. The Message arrives late, and the normal duplicate check keeps it from becoming a second Message.

### Threat boundary

A `0600` file is readable by every process of the same user. A key in the Secret Service is too. The encryption keeps the stores and Media away from indexers, backup or sync tools and sandboxed apps. It does not keep them from a hostile native process of the same user.

The Daemon reads the store key through go-keyring, which opens a 'plain' Secret Service session. The key then crosses the session bus in clear text, and a bus monitor of the same user can read it.

Other local users can read the arguments of every process (`/proc/<pid>/cmdline`, because `/proc` has no `hidepid`). So whisker never passes message text, names or Chat ids as arguments to show a notification. The Daemon shows notifications itself over D-Bus (section 4). The click command of a notification holds an opaque token, not the Chat id (`notification.open` in `docs/contract.md`). Two places still put data in arguments: whisker passes the path of a plain Media copy to mpv or `xdg-open` (the names are opaque keyed hashes), and it passes the URL of a map link to `xdg-open`, which hands it to the browser. The coordinates in that URL then stay in the browser arguments. Nothing else goes into arguments.

Notification text leaves the encrypted stores. Omarchy's notification service saves the summary, the body and the click command of each notification in plain text in `~/.local/state/omarchy/notifications/live.json` and `history.json`, for up to 24 h. With "Show preview" on (the default), that is the Chat name, the sender and the message text. With it off, it is the Chat name and "New message". whisker does not withdraw a notification when a Message is revoked, deleted or expires: Omarchy moves a toast into its history after a few seconds, so a withdrawal cannot remove the text.

On a default Omarchy install, the default keyring (`Default_keyring`) has no password, and gnome-keyring stores it in plain text in `~/.local/share/keyrings/`. A backup of `~/.local/share` then holds the stores and their key. So the store encryption protects against backups, sync tools and sandboxed apps only when the keyring has a password. A sandboxed app that can reach `org.freedesktop.secrets` can read the item from any unlocked collection.

The Daemon checks the header of the default keyring file (`keystore.PlainKeyring`) on each start and sends `keyringPlain` in `ready`. The Plugin then shows a notice in Settings. The README tells users to set a password and, after a possible leak, to Log out and link again.

whatsmeow does not check the signed device lists of contacts or of the user's own account, and it accepts a changed identity key without asking (`AutoTrustIdentity`). The Companion Device trusts the WhatsApp server for the end-to-end keys. Against a malicious server it is weaker than the official apps. For this, the Daemon shows two System Messages: "Security code changed" in the Direct Chat of a contact, and "A new device was linked to your account" in the user's own Direct Chat. The Daemon reads the own device list at each connection, because whatsmeow sends no event for it. The first list after a new link only fills the stored set.

Deletion: both stores use `secure_delete(1)`, so SQLite overwrites a deleted row in the store file. On the first start after the update, `Profile.Open` runs `VACUUM` once on both stores to clear old free space. The `VACUUM` copy stays in memory (`temp_store(2)`). The rollback journal is a separate file. The filesystem can keep its (encrypted) blocks until it reuses them.

The Media folders and files carry keyed names: `hex(HMAC(k, "chat\0" + chat))` for the Chat folder and `hex(HMAC(k, "file\0" + chat + "\0" + id))` for the file. `k` is an HKDF-SHA256 subkey of the store key (info `whisker media names`). Plain copies use the same names. The Media folder still shows the number of Media files per Chat and their sizes. It shows no names or ids. File and folder modification times are the Unix epoch. The change time (ctime) still shows when a file arrived.

### Lock

The Profile lock is in `$XDG_RUNTIME_DIR/whisker/` (see section 3).

## 6. Linking

- The Daemon runs linking (QR code and Pair Code) and streams its state to the Plugin. The Plugin shows it.
- The Daemon renders the QR code with a pure-Go library. It sends the code as a `data:image/png;base64,...` URI. Qt `Image` loads it natively.
- The Daemon sends the Pair Code as text.
- Each linking makes a new Companion Device. After linking, History Sync brings back recent Chats and Messages.

## 7. Plugin and Daemon contract

- **Transport.** Stdio, as in section 3.
- **Framing.** Newline-delimited JSON. The Daemon reads with `bufio.Scanner` and a 1 MiB buffer cap. The cap fits WhatsApp's 65,536-character text limit. On an oversized line, the Daemon logs the error and exits with a distinct code. The Plugin shows that error and does not restart in a loop. The Plugin reads with no cap.
- **Bounded frames.** History comes in pages. Frames carry no Media bytes.
- **Envelope.** JSON-RPC 2.0.
  - A request has `id`, `method` and `params`.
  - A response has `result` or `error` (`code`, `message`, `data`).
  - Daemon events are notifications.
  - Error codes are stable application codes.
  - The Daemon sends a `ready` notification once it can take commands.
- **Versioning.** The wire carries no version and no `hello`. The rule is an exact release-version match (see section 8). A Plugin always runs its own Daemon, so a mixed pair cannot occur.
- **Single source of truth.** A hand-written `docs/contract.md` lists the methods and notifications. The golden fixtures in `contract/fixtures/*.jsonl` back it with tests.
- **QR code.** A `data:` URI (section 6).
- **Media.** The Daemon sends a file path on request, never bytes. The path points to the plain copy described in section 5.

## 8. Distribution and release

### Install

`omarchy plugin add <url>` clones the whole repo and moves it to `~/.config/omarchy/plugins/<manifest id>`. `omarchy plugin update` runs `git fetch origin HEAD` and a fast-forward merge. Omarchy has no build step, no install hook and no binary download. The Plugin id is `pauwelz.whisker`. `manifest.json` must sit at the repo root, and the repo must hold no symlink.

### Binary delivery

- The Plugin downloads `whisker-daemon` from the GitHub release with `scripts/fetch-daemon.sh`. The script downloads to a temp file, verifies it and moves it into place.
- The script verifies the binary against a `SHA256SUMS` file committed in the repo. The clone is the trust root.
- The repo holds no binary and has no source-build fallback.
- `bin/` is git-ignored and holds the binary. A fast-forward update keeps it. `omarchy plugin remove` deletes it. A local dev build writes the same path with the same version, so development needs no download.
- The only target is `linux/amd64`. `SHA256SUMS` has one line per architecture, so `arm64` can be added later.

### Version

The Plugin and the Daemon share one version. `manifest.json` is the single source. The build embeds it in the Daemon. Before it starts the Daemon, the Plugin runs `whisker-daemon --version`. On a mismatch, it downloads the matching binary. It stops and replaces a running Daemon that has the wrong version. The Plugin never runs a binary with the wrong version.

### Failure

If the download fails, the Plugin shows "Daemon download failed" with a retry action. It retries at the next shell start.

### Release flow

1. Work happens on `dev`.
2. To release version V, the maintainer runs the Release workflow on `dev` with V. The workflow bumps `manifest.json`, moves the `[Unreleased]` entries in `CHANGELOG.md` to a `[V]` section, builds, and publishes release `vV` with the raw binary as its asset.
3. The workflow commits `SHA256SUMS` on top and checks the release with `scripts/fetch-daemon.sh`.
4. For a final version, the workflow fast-forwards `main` (the default branch) to that commit.

The steps are in [releasing.md](releasing.md).

`main` only points at published releases, so HEAD is always installable.

### Marketplace

A marketplace listing is the goal. The repo has a root README with install and removal steps, and a root license. Removal keeps the Profile data in the XDG paths. The README says how to delete it. The README says nothing about whisker-legacy.

## 9. Open points

- Decided since this spec began, see [contract.md](contract.md):
  - The Profile lock, its wait time and the exit codes: section "Exit codes".
  - The storage format of Profile Settings: section `settings.get`. The file is `$XDG_CONFIG_HOME/whisker/settings.json`.
  - The application error codes: section "Errors".
  - The chat schema and its migrations: section "Chat store schema". Search filters in Go, so FTS5 is not used.
- The package split is the one in `daemon/internal/`. Section 4 names the modules.
- A locked keyring is only tested with a fake backend. A run with `go-keyring` on a real locked keyring is open.
- The marketplace scanner is not checked for how it treats a verified download.
- An `arm64` target is not planned. It needs a new `SHA256SUMS` line and a new asset.

## 10. Decisions this rests on

- [How Omarchy installs plugins from Git and ships binaries](https://github.com/Pauwelz/whisker-legacy/issues/99)
- [Omarchy product: repo layout and distribution](https://github.com/Pauwelz/whisker-legacy/issues/100)
- [Omarchy Daemon: language, shape and lifecycle](https://github.com/Pauwelz/whisker-legacy/issues/103) (stdio replaces its socket, see the next item)
- [Plugin and Daemon contract](https://github.com/Pauwelz/whisker-legacy/issues/104)
- [Encrypted whatsmeow store: pure Go or SQLCipher](https://github.com/Pauwelz/whisker-legacy/issues/106)
- [Encrypt the Profile's stores at rest?](https://github.com/Pauwelz/whisker-legacy/issues/107)
- [Moving existing users to the new products](https://github.com/Pauwelz/whisker-legacy/issues/112) (the Plugin id `pauwelz.whisker`, and no migration)
