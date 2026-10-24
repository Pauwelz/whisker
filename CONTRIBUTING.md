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

## Releases

The maintainer releases by fast-forwarding `main` to `dev`.
Contributors do not release.
