# whisker

A WhatsApp client for the Omarchy shell: a Quickshell Plugin and its own Go Daemon, built on whatsmeow. Linux only.

whisker is in early development and is not ready to install. The architecture is in [docs/architecture.md](docs/architecture.md).

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
