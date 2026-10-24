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

## Release steps

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
   Use `--prerelease` for an `-rc` version. Omit it for a final version.

   ```sh
   gh release create "v$V" bin/whisker-daemon-linux-amd64 \
     --target "$(git rev-parse HEAD)" --prerelease \
     --title "v$V" --notes "See CHANGELOG.md."
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
