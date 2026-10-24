const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");

const ASSET = "whisker-daemon-linux-amd64";
const BODY = "fake daemon binary";
const sha = (text) => crypto.createHash("sha256").update(text).digest("hex");

// Seam: scripts/fetch-daemon.sh in a temp plugin tree, with a file:// release server.
function run({ manifest = '{\n  "version": "1.2.3"\n}', sums, served = BODY } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "whisker-fetch-"));
  fs.mkdirSync(path.join(dir, "scripts"));
  fs.copyFileSync(path.join(__dirname, "..", "scripts", "fetch-daemon.sh"), path.join(dir, "scripts", "fetch-daemon.sh"));
  fs.writeFileSync(path.join(dir, "manifest.json"), manifest);
  if (sums !== undefined) fs.writeFileSync(path.join(dir, "SHA256SUMS"), sums);
  const server = path.join(dir, "server", "v1.2.3");
  fs.mkdirSync(server, { recursive: true });
  if (served !== null) fs.writeFileSync(path.join(server, ASSET), served);
  const result = spawnSync("sh", [path.join(dir, "scripts", "fetch-daemon.sh")], {
    env: { ...process.env, WHISKER_RELEASE_URL: "file://" + path.join(dir, "server") },
  });
  return { status: result.status, bin: path.join(dir, "bin", "whisker-daemon") };
}

const goodSums = `${sha(BODY)}  ${ASSET}\n`;

test("a matching download lands in bin/ and is executable", () => {
  const r = run({ sums: goodSums });
  assert.equal(r.status, 0);
  assert.equal(fs.readFileSync(r.bin, "utf8"), BODY);
  assert.ok(fs.statSync(r.bin).mode & 0o100);
});

test("no version in the manifest exits 20", () => {
  assert.equal(run({ manifest: "{}", sums: goodSums }).status, 20);
});

test("a missing SHA256SUMS exits 21", () => {
  assert.equal(run({}).status, 21);
});

test("a SHA256SUMS without the asset line exits 21", () => {
  assert.equal(run({ sums: `${sha(BODY)}  other-file\n` }).status, 21);
});

test("a failed download exits 22 and leaves no binary", () => {
  const r = run({ sums: goodSums, served: null });
  assert.equal(r.status, 22);
  assert.equal(fs.existsSync(r.bin), false);
});

test("a changed byte exits 23 and leaves no binary or temp file", () => {
  const r = run({ sums: goodSums, served: BODY + "x" });
  assert.equal(r.status, 23);
  assert.equal(fs.existsSync(r.bin), false);
  assert.deepEqual(fs.readdirSync(path.dirname(r.bin)), []);
});
