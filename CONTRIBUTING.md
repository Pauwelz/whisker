# Contributing to whisker

Read `AGENTS.md` and `docs/architecture.md` before you change code.
`GLOSSARY.md` defines the terms. `CODING_STANDARDS.md` defines the code rules.

## Send a pull request

1. Fork the repo and create a branch from `dev`.
2. Make your change.
3. Add an entry under `[Unreleased]` in `CHANGELOG.md`.
4. Push the branch and open a pull request that targets `dev`.
5. Set the PR title to a [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/) message. Example: `feat: show unread count in the bar`.

The maintainer merges each PR with squash.
GitHub uses the PR title as the commit message on `dev`.
So the PR title must follow Conventional Commits.

## Changelog entry

Add the entry in the same PR as the change.
Use the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) groups: Added, Changed, Deprecated, Removed, Fixed, Security.
Skip the entry for changes that users do not see, such as internal docs.

## Retarget a PR that targets `main`

GitHub can open a PR against `main`, the default branch.
`main` holds only published releases. Do not target it.

1. Open the PR on GitHub.
2. Click **Edit** next to the PR title.
3. Select `dev` in the base branch list.
4. Click **Save**.

## Build and run the Daemon

Work happens on `dev`. `main` only moves to release commits, so its HEAD is always installable.
The architecture is in [docs/architecture.md](docs/architecture.md).

```sh
scripts/build-daemon.sh        # writes bin/whisker-daemon, with the version from manifest.json
bin/whisker-daemon --version
bin/whisker-daemon             # type JSON lines; press Ctrl+D to stop
```

The Plugin runs `bin/whisker-daemon`. Edited Plugin QML loads only after a shell restart, because the Service is `keepLoaded`.

## Tests

```sh
cd daemon && go test ./...     # Go: the Daemon, driven by the golden fixtures
node --test tests/*.test.js    # Plugin state logic, driven by the same fixtures
```

To add a golden fixture, follow [docs/contract.md](docs/contract.md), section "Golden fixtures".

## Take screenshots

The demo Daemon shows made-up Chats, so a screenshot shows no real data.
It keeps its Profile in `$XDG_RUNTIME_DIR/whisker-demo` and does not touch your real Profile or keyring.

1. Run `scripts/demo-daemon.sh`.
2. Restart the shell.
3. Take two screenshots of the pop-out: the Chat list and the "Weekend hike" Chat.
4. Run `scripts/make-preview.sh <chat-list.png> <chat.png>`. It writes `preview.png`, which the README and the marketplace card use.
5. Run `scripts/build-daemon.sh`.
6. Restart the shell.

## Releases

The maintainer releases with the steps in [docs/releasing.md](docs/releasing.md).
Contributors do not release.
