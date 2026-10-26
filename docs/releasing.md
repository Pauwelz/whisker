# Releasing whisker

Only the maintainer releases. Run every command from the repo root.
The version V is in SemVer form, for example `1.0.0-rc.1`.
Architecture background is in [architecture.md](architecture.md), section 8.

## The fetch script

`scripts/fetch-daemon.sh` is POSIX `sh` and needs `curl` and `sha256sum`.
It downloads `whisker-daemon-linux-amd64` from the release `v<manifest version>` to a temp file in `bin/`.
It checks the file against the `SHA256SUMS` line for that asset and moves it to `bin/whisker-daemon`.
It never leaves a partial binary in `bin/`.

| Code | Meaning |
|---|---|
| 0 | OK. `bin/whisker-daemon` is in place. |
| 20 | `manifest.json` has no version. |
| 21 | `SHA256SUMS` is missing or has no line for the asset. |
| 22 | The download failed. |
| 23 | The checksum did not match. |

The Plugin runs the script when the binary is missing or has the wrong version.
Exit codes other than 0 show "Daemon download failed" with Retry.
The Plugin runs the script again at the next shell start.

`WHISKER_RELEASE_URL` replaces the release base URL. Only the tests use it.

## Release with GitHub Actions

The Release workflow (`.github/workflows/release.yml`) does all release steps.

1. Open **Actions** > **Release** on GitHub.
2. Click **Run workflow**, select the branch `dev`, and type V.
3. Wait for the run to pass.

The workflow stops before it changes anything when:

- it does not run on `dev`,
- V does not match the SemVer regex from semver.org,
- the tag `vV` already exists,
- `CHANGELOG.md` has no entries under `[Unreleased]`.

Then it does the manual steps below in the same order, as `github-actions[bot]`.
It pushes the release commit to `dev` before it publishes the release.
If `dev` moved after the run started, this push fails and nothing is published.
The release notes are the `[V]` section of `CHANGELOG.md`.
A V with a pre-release part, such as `1.0.0-rc.1`, becomes a pre-release and does not move `main`.

## Manual release steps

Use these steps only when the workflow cannot run.

1. Start on a clean, current `dev` and run the tests.

   ```sh
   git switch dev
   git pull --ff-only
   git status --porcelain          # must print nothing
   (cd daemon && go test ./...) && node --test tests/*.test.js
   ```

2. Bump the version in `manifest.json` and move the `[Unreleased]` entries in `CHANGELOG.md` to a `[V]` section.

   ```sh
   V=1.0.0-rc.1
   sed -i "s/^\( *\"version\": *\"\)[^\"]*\"/\1$V\"/" manifest.json
   sed -i "s/^## \[Unreleased\]$/## [Unreleased]\n\n## [$V] - $(date +%F)/" CHANGELOG.md
   git diff                        # check both files
   git commit -am "chore(release): $V"
   git push origin dev
   ```

3. Build the Daemon with the new version, and name it as the release asset.

   ```sh
   scripts/build-daemon.sh
   bin/whisker-daemon --version    # must print V
   cp bin/whisker-daemon bin/whisker-daemon-linux-amd64
   ```

4. Publish the release `vV` with the raw binary as its asset. The tag points at the release commit.
   The release notes are the `[V]` section of `CHANGELOG.md`, from `scripts/release-notes.sh`.
   Use `--prerelease` for an `-rc` version. Omit it for a final version.

   ```sh
   scripts/release-notes.sh "$V" > bin/notes.md
   gh release create "v$V" bin/whisker-daemon-linux-amd64 \
     --target "$(git rev-parse HEAD)" --prerelease \
     --title "v$V" --notes-file bin/notes.md
   ```

5. Commit `SHA256SUMS` on top of the release commit.

   ```sh
   (cd bin && sha256sum whisker-daemon-linux-amd64) > SHA256SUMS
   git add SHA256SUMS
   git commit -m "chore(release): add SHA256SUMS for $V"
   git push origin dev
   ```

6. Check the release. Clone it in an empty directory, run `scripts/fetch-daemon.sh`, then `bin/whisker-daemon --version`.

7. For a final version only, fast-forward `main` to the `SHA256SUMS` commit.
   A pre-release never moves `main`.

   ```sh
   git push origin dev:main
   ```

`SHA256SUMS` has one line per architecture. v1 has only `linux/amd64`.

`scripts/build-daemon.sh` now builds with `-trimpath`, and `daemon/go.mod` pins the toolchain. The binary hash then differs from earlier releases. The next release must regenerate `SHA256SUMS` from a new build.
