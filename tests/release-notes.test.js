const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const CHANGELOG = `# Changelog

## [Unreleased]

## [1.1.0] - 2026-11-01

### Fixed

- A fix.

## [1.1.0-rc.1] - 2026-10-30

- The rc.

## [1.0.0] - 2026-10-26
`;

// Seam: scripts/release-notes.sh in a temp tree with its own CHANGELOG.md.
function run(version) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "whisker-notes-"));
  fs.mkdirSync(path.join(dir, "scripts"));
  fs.copyFileSync(path.join(__dirname, "..", "scripts", "release-notes.sh"), path.join(dir, "scripts", "release-notes.sh"));
  fs.writeFileSync(path.join(dir, "CHANGELOG.md"), CHANGELOG);
  const r = spawnSync("sh", [path.join(dir, "scripts", "release-notes.sh"), version], { encoding: "utf8" });
  return { status: r.status, out: r.stdout };
}

test("prints the section of the version, without its heading", () => {
  assert.deepEqual(run("1.1.0"), { status: 0, out: "### Fixed\n\n- A fix.\n" });
});

test("does not match a version that only starts with V", () => {
  assert.deepEqual(run("1.1.0-rc.1"), { status: 0, out: "- The rc.\n" });
});

test("an empty or missing section exits 1", () => {
  assert.equal(run("1.0.0").status, 1);
  assert.equal(run("9.9.9").status, 1);
});
