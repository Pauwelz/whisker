# whisker

A WhatsApp client for the Omarchy shell: a Quickshell Plugin and its own Go Daemon, built on whatsmeow. Linux only.

whisker is in early development. The architecture is in [docs/architecture.md](docs/architecture.md).

## Install and remove

```sh
omarchy plugin add https://github.com/Pauwelz/whisker   # install
omarchy plugin remove pauwelz.whisker                    # remove the Plugin and its Daemon binary
```

The Plugin downloads its Daemon on first start and checks it against `SHA256SUMS`.

To attach a file or a screenshot with Ctrl+V, whisker needs `wl-clipboard` (`wl-paste`). Omarchy installs it. Without it, Ctrl+V pastes text only.

## Your data

whisker keeps the Profile in these places:

| Place | Holds |
|---|---|
| `$XDG_DATA_HOME/whisker` (default `~/.local/share/whisker`) | The encrypted device store, chat store and Media. |
| `$XDG_CONFIG_HOME/whisker` (default `~/.config/whisker`) | Profile Settings, as plain `settings.json`. |
| The login keyring, item `whisker` / `store-key` | The key that opens the stores. |

A pasted image waits in `$XDG_RUNTIME_DIR/whisker/pasted` (in memory) until you send or remove it.

Log out in the pop-out deletes the Profile and the key. It keeps the Profile Settings. `omarchy plugin remove` does not delete any of these.
To delete everything by hand, remove both folders and the keyring item.

The key protects the stores only as well as your keyring. Set a password on the login keyring. Without one, a person with your disk can read the key.

## Keyboard shortcuts

The pop-out has these fixed shortcuts:

| Key | Action |
|---|---|
| Escape | Go back one level: close the open Chat, then the settings view, then the pop-out. |
| Ctrl+F | Focus the search field of the Chat list. |
| Enter | Send the composer text. |
| Shift+Enter | Add a line in the composer. |
| Ctrl+V | Paste in the composer. A file copied in a file manager, or an image such as a screenshot, becomes the Attachment. |

## Development

Work happens on `dev`. `main` only moves to release commits, so its HEAD is always installable.

### Build and run the Daemon

```sh
scripts/build-daemon.sh        # writes bin/whisker-daemon, with the version from manifest.json
bin/whisker-daemon --version
bin/whisker-daemon             # type JSON lines; press Ctrl+D to stop
```

The Plugin runs `bin/whisker-daemon`. Edited Plugin QML loads only after a shell restart, because the Service is `keepLoaded`.

### Tests

```sh
cd daemon && go test ./...     # Go: the Daemon, driven by the golden fixtures
node --test tests/*.test.js    # Plugin state logic, driven by the same fixtures
```

To add a golden fixture, follow [docs/contract.md](docs/contract.md), section "Golden fixtures".

## License

[MIT](LICENSE)
