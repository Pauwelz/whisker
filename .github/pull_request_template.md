<!-- The rules are in CONTRIBUTING.md. -->

- [ ] The PR targets `dev`.
- [ ] The PR title is a Conventional Commit message.
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]`, or the change is not user-visible.
- [ ] `cd daemon && go test ./...` and `node --test tests/*.test.js` pass.
- [ ] `scripts/check-omarchy.sh` passes, if the Plugin QML uses a new Omarchy API.
