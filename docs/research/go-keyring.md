# go-keyring on the Secret Service

Scope: `github.com/zalando/go-keyring` v0.2.8 and its use in `docs/architecture.md` section 5 items 3-4.
Research date: 2026-10-24. Source only. No code ran and no keyring was touched.

## Answer

**1. Transport, blocking, timeout.**
On Linux, go-keyring talks to the Secret Service over the D-Bus session bus with `github.com/godbus/dbus/v5` v5.2.2 ([go.mod](https://github.com/zalando/go-keyring/blob/v0.2.8/go.mod), [`NewSecretService`](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L50-L60)).
It needs no cgo on Linux: the `linux` build tag has no `cgo` condition ([`keyring_unix.go` L1](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L1)).
It uses the `plain` session algorithm, so the secret crosses the bus unencrypted ([`OpenSession`](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L63-L72)).
`Get` makes these calls: `GetProperty(Collections)` (to pick the collection), `Service.Unlock`, `Collection.SearchItems`, `Service.OpenSession`, `Item.Unlock`-via-`Service.Unlock`, `Item.GetSecret` ([`Get`](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L105-L135), [`findItem`](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L54-L77)).
`Set` calls `OpenSession`, `Unlock`, `CreateItem` ([`Set`](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L16-L51)).
Every call uses `Object.Call(..., 0, ...)`, which blocks on the reply with `context.Background()` ([godbus `Call`](https://github.com/godbus/dbus/blob/v5.2.2/object.go#L33-L35)).
No go-keyring function takes a `context.Context`, and the public API has none ([`keyring.go`](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring.go)).
godbus has `CallWithContext` ([godbus](https://github.com/godbus/dbus/blob/v5.2.2/object.go#L38-L40)), but go-keyring never uses it.
The longest block is not a method call. After `Unlock` returns a prompt path, `handlePrompt` calls `Prompt()` and then waits on `signal := <-promptSignal` with no timeout ([`handlePrompt` L187-219](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L187-L219)).
It waits until the user completes or dismisses the prompt. No D-Bus `reply_timeout` applies, because a signal wait is not a method call.
A Go caller cannot cancel it. The safe pattern is: run `keyring.Get` in a goroutine, send the result on a buffered channel (size 1), and `select` on that channel and a 30 s timer.
On timeout the goroutine stays blocked until the prompt ends or the process exits. This is a bounded leak: allow one in-flight attempt, and let Retry join it instead of starting a second one.
One side effect needs care: a timed-out `Set` can still finish later, when the user answers the prompt, and write the key. On Retry, call `Get` first and treat `ErrNotFound` as the only "no key yet" signal. Never keep a key that was generated before a timed-out `Set`.
The only way to get true cancellation is to skip go-keyring and call godbus with `CallWithContext` yourself, and call `org.freedesktop.Secret.Prompt.Dismiss` on timeout ([spec: Prompt](https://specifications.freedesktop.org/secret-service-spec/latest/org.freedesktop.Secret.Prompt.html)). That is about 80 lines of new code and is not in the current spec.

**2. Failure cases.**
(a) No session bus: `dbus.SessionBus()` returns an error when it cannot find a bus address ([`SessionBus`](https://github.com/godbus/dbus/blob/v5.2.2/conn.go#L61-L75), [`getSessionBusAddress`](https://github.com/godbus/dbus/blob/v5.2.2/conn.go#L77-L88)). `Get` and `Set` return that error.
(a) Bus present, no provider and no activation file: the first call to `org.freedesktop.secrets` returns a D-Bus error from the bus (expected `org.freedesktop.DBus.Error.ServiceUnknown`; not confirmed). go-keyring returns it unchanged. It is not `ErrNotFound`.
(a) Bus present, activation file present: the bus starts the provider and the call waits for it. The wait ends at the bus `service_start_timeout` ([dbus-daemon config](https://dbus.freedesktop.org/doc/dbus-daemon.1.html)). The default value is not confirmed here. Your 30 s timer covers this case.
(b) Collection locked, prompt shown: `Unlock` returns a prompt path and go-keyring blocks on the `Completed` signal (see answer 1). The call returns the real result when the user enters the password.
(c) User dismisses the prompt: the spec says the service emits `Completed(dismissed=true, ...)` ([spec: Prompts](https://specifications.freedesktop.org/secret-service-spec/latest/prompts.html)). go-keyring drops the `dismissed` flag (`_, v, err := s.handlePrompt(prompt)`, [L114](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L106-L130)). The collection stays locked, so `unlocked` is empty. The check `len(unlocked) != 1` then fails and `Unlock` returns `fmt.Errorf("failed to unlock correct collection '%v'", ...)` ([L125-127](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L106-L130)). This error has no sentinel value. It is not `ErrNotFound`. Whisker can match it only as "any error that is not `ErrNotFound`".
(d) Default collection missing: `GetLoginCollection` checks for `/org/freedesktop/secrets/collection/login`. If absent, it falls back to the alias `/org/freedesktop/secrets/aliases/default` and ignores the check error ([L97-103](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L97-L103)). If the alias also points nowhere, the next `Unlock` call fails with a D-Bus error. The spec lists `org.freedesktop.Secret.Error.NoSuchObject` for "no such item or collection" ([spec: Errors](https://specifications.freedesktop.org/secret-service-spec/latest/errors.html)). Which error gnome-keyring returns here is not confirmed. The README says go-keyring expects the `login` collection to exist ([README](https://github.com/zalando/go-keyring/blob/v0.2.8/README.md)).
`ErrNotFound` has one source: `SearchItems` returns zero matches ([`findItem` L72-74](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L54-L77)). It means "keyring works, key absent". It is the first-run signal. It never means "keyring unavailable".

**3. What Omarchy ships.**
Omarchy installs `gnome-keyring` and `libsecret` ([`omarchy-base.packages`](https://github.com/basecamp/omarchy/blob/v4.0.6/install/omarchy-base.packages), lines 44 and 70). gnome-keyring registers the `org.freedesktop.secrets` bus name with D-Bus activation: `gnome-keyring-daemon --start --foreground --components=secrets` ([service file](https://github.com/GNOME/gnome-keyring/blob/e970619902bf662ce3f182e87f3f7dda00caff43/daemon/org.freedesktop.secrets.service.in)).
PAM does not unlock the keyring at login. The Omarchy SDDM step deletes the `pam_gnome_keyring.so` lines from `/etc/pam.d/sddm`, so password logins do not create or unlock an encrypted login keyring ([`install/login/sddm.sh`](https://github.com/basecamp/omarchy/blob/v4.0.6/install/login/sddm.sh)). I did not find `pam_gnome_keyring` in the lock-screen PAM file either ([`bin/omarchy-apply-lock`](https://github.com/basecamp/omarchy/blob/v4.0.6/bin/omarchy-apply-lock), first 40 lines only).
Instead Omarchy creates a keyring with no password. `install/user/default-keyring.sh` writes `~/.local/share/keyrings/Default_keyring.keyring` and a `default` file that names it ([script](https://github.com/basecamp/omarchy/blob/v4.0.6/install/user/default-keyring.sh)). The collection is named `Default_keyring`, not `login`. So go-keyring takes the `default` alias path ([L97-103](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L97-L103)).
Result on Omarchy: a normal install has no password, so no lock and no prompt. The "locked" case only occurs if the user replaced this keyring with a password-protected one.
Risk to the spec: a keyring with no password most likely stores secrets in plain text in that file (inferred from the plain-text `[keyring]` header; not confirmed). If so, "the key never goes on disk" in section 5 item 3 is false on a default Omarchy install. Check this before you rely on the claim.

**4. Size and encoding.**
The API takes and returns a Go `string` ([`keyring.go`](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring.go)). On the wire, go-keyring sends the bytes with content type `text/plain; charset=utf8` ([`NewSecret` L34-41](https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L34-L41)).
The documented limit on Linux and Unix: no theoretical limit, but performance suffers above 100 KiB ([`ErrSetDataTooBig` comment](https://github.com/zalando/go-keyring/blob/v0.2.8/keyring.go)). A 32-byte key is far below any limit.
Random bytes are not valid UTF-8 in general, and the declared type is text. Store the key as base64 (44 chars) or hex (64 chars) and decode after `Get`. This also avoids provider-side text handling that no source here rules out. Raw storage is not confirmed safe.

## Sources

go-keyring v0.2.8 (tag):
- <https://github.com/zalando/go-keyring/blob/v0.2.8/go.mod>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/keyring.go>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L1>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L16-L51>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L54-L77>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/keyring_unix.go#L105-L135>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L50-L60>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L63-L72>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L34-L41>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L97-L103>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L106-L130>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/secret_service/secret_service.go#L187-L219>
- <https://github.com/zalando/go-keyring/blob/v0.2.8/README.md>

godbus v5.2.2 (tag):
- <https://github.com/godbus/dbus/blob/v5.2.2/object.go#L33-L35>
- <https://github.com/godbus/dbus/blob/v5.2.2/object.go#L38-L40>
- <https://github.com/godbus/dbus/blob/v5.2.2/conn.go#L61-L75>
- <https://github.com/godbus/dbus/blob/v5.2.2/conn.go#L77-L88>

Specs and daemons:
- <https://specifications.freedesktop.org/secret-service-spec/latest/prompts.html>
- <https://specifications.freedesktop.org/secret-service-spec/latest/org.freedesktop.Secret.Prompt.html>
- <https://specifications.freedesktop.org/secret-service-spec/latest/errors.html>
- <https://dbus.freedesktop.org/doc/dbus-daemon.1.html>
- <https://github.com/GNOME/gnome-keyring/blob/e970619902bf662ce3f182e87f3f7dda00caff43/daemon/org.freedesktop.secrets.service.in>

Omarchy v4.0.6 (tag; the default branch is `quattro`):
- <https://github.com/basecamp/omarchy/blob/v4.0.6/install/omarchy-base.packages>
- <https://github.com/basecamp/omarchy/blob/v4.0.6/install/login/sddm.sh>
- <https://github.com/basecamp/omarchy/blob/v4.0.6/install/user/default-keyring.sh>
- <https://github.com/basecamp/omarchy/blob/v4.0.6/bin/omarchy-apply-lock>

## Not confirmed (needs a run)

Run these on a throwaway session bus and keyring (for example `dbus-run-session` with a temporary `XDG_DATA_HOME`), never on the real keyring.
- The exact error name for no provider (`ServiceUnknown`?) and for a missing default collection (`NoSuchObject`?).
- That a dismissed prompt gives the "failed to unlock correct collection" error and does not hang.
- The `service_start_timeout` default, and what happens when the provider starts but never answers.
- That the `default` alias resolves to `Default_keyring` in gnome-keyring, and that `Unlock` on the alias path passes the `len(unlocked) != 1` check.
- That a timed-out `Set` still writes the item after the user answers late, and that the blocked goroutine then exits.
- That a 32-byte raw binary value survives a round trip; base64 avoids the question.
- Whether `Default_keyring.keyring` with no password stores the key in plain text on disk.
- That gnome-keyring starts through D-Bus activation on a fresh Omarchy login (the Omarchy files read do not start it explicitly).
- That `DBUS_SESSION_BUS_ADDRESS` is set when Quickshell starts the Daemon. The Daemon inherits it from Quickshell.
