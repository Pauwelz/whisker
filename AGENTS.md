## Agent skills

### Issue tracker

Issues and specs are tracked in GitHub Issues (`Pauwelz/whisker`) using the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the five canonical triage labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`GLOSSARY.md` and `docs/adr/` at the repo root). See `docs/agents/domain.md`.

### Architecture

`docs/architecture.md` is the architecture spec this repo starts from. Read it before you add a module or a dependency.

### Coding standards

Review against `CODING_STANDARDS.md`.

### Versions, commits and changelog

- Version releases with [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html). `manifest.json` holds the version (see `docs/architecture.md`, section 8).
- Write commit messages with [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/).
- Keep `CHANGELOG.md` with [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/). Add each user-facing change under `[Unreleased]` in the same commit as the change.
