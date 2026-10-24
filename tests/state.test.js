const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const load = require("./load");

const State = load("WhiskerState.js");
const root = path.join(__dirname, "..");
const fixtureDir = path.join(root, "contract", "fixtures");

// Seam B: fold the Daemon frames of each golden fixture, and check the view state at each "view" line.
for (const name of fs.readdirSync(fixtureDir).filter((f) => f.endsWith(".jsonl"))) {
  test(`fixture ${name}`, () => {
    let state = State.initialState();
    const lines = fs.readFileSync(path.join(fixtureDir, name), "utf8").split("\n").filter(Boolean);
    for (const text of lines) {
      const line = JSON.parse(text);
      if (line.dir === "out") state = State.applyFrame(state, line.frame);
      if (line.dir === "view") {
        for (const [key, value] of Object.entries(line.expect)) assert.equal(state[key], value, key);
      }
    }
  });
}

test("the Plugin starts on the starting screen", () => {
  assert.equal(State.initialState().screen, "starting");
});

test("a version mismatch shows an error screen", () => {
  const state = State.checkVersion("1.2.3", "1.2.4\n");
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "version-mismatch");
});

test("a matching version allows the start", () => {
  assert.equal(State.checkVersion("1.2.3", "1.2.3\n"), null);
});

test("a binary that does not run shows an error screen", () => {
  assert.equal(State.checkVersion("1.2.3", "").reason, "binary-missing");
});

test("an oversized line exit shows its own error screen", () => {
  const state = State.daemonExited(State.initialState(), 10);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "oversized-line");
});

test("any other Daemon exit shows an error screen", () => {
  const state = State.daemonExited(State.applyFrame(State.initialState(), { method: "ready" }), 1);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "daemon-exited");
});

test("a Profile in use exit shows its own error screen", () => {
  const state = State.daemonExited(State.initialState(), 11);
  assert.equal(state.screen, "error");
  assert.equal(state.reason, "profile-in-use");
});

test("a clean Daemon exit shows the stopped screen", () => {
  assert.equal(State.daemonExited(State.initialState(), 0).reason, "daemon-exited");
});

test("the contract doc lists the Profile in use code", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${State.EXIT_PROFILE_IN_USE} |`));
});

test("parseLine ignores a line that is not JSON", () => {
  assert.equal(State.parseLine("log noise"), null);
  assert.equal(State.parseLine('{"method":"ready"}').method, "ready");
});

test("a matching version starts the Daemon", () => {
  assert.equal(State.versionStep("1.2.3", "1.2.3\n", false).action, "start");
});

test("a missing or wrong-version binary is downloaded once", () => {
  for (const output of ["", "1.2.4\n"]) {
    const step = State.versionStep("1.2.3", output, false);
    assert.equal(step.action, "download");
    assert.equal(step.view.screen, "starting");
  }
});

test("a binary that is still wrong after the download shows an error screen", () => {
  const step = State.versionStep("1.2.3", "1.2.4\n", true);
  assert.equal(step.action, "fail");
  assert.equal(step.view.reason, "version-mismatch");
});

test("a failed download shows the Retry error screen", () => {
  for (const code of [20, 21, 22, 23, 99]) {
    const state = State.downloadFailed(code);
    assert.equal(state.screen, "error");
    assert.equal(state.reason, "download-failed");
    assert.equal(state.canRetry, true);
    assert.ok(state.message.startsWith("Daemon download failed"));
  }
  assert.ok(State.downloadFailed(23).message.includes("checksum"));
});

test("other error screens offer no Retry", () => {
  assert.notEqual(State.checkVersion("1.2.3", "").canRetry, true);
});

test("docs/releasing.md lists the fetch script exit codes", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "releasing.md"), "utf8");
  for (const code of Object.keys(State.FETCH_EXIT_MESSAGES)) assert.ok(doc.includes(`| ${code} |`), code);
});

test("the contract doc lists the exit code the Plugin maps", () => {
  const doc = fs.readFileSync(path.join(root, "docs", "contract.md"), "utf8");
  assert.ok(doc.includes(`| ${State.EXIT_OVERSIZED_LINE} |`));
});

test("an unavailable keyring shows a Retry action", () => {
  const state = State.applyFrame(State.initialState(), { jsonrpc: "2.0", method: "keyringUnavailable", params: {} });
  assert.equal(state.screen, "keyring-unavailable");
  assert.equal(state.canRetry, true);
});

test("the Retry request is a keyring.retry line", () => {
  const line = State.keyringRetryRequest(7);
  assert.ok(line.endsWith("\n"));
  assert.deepEqual(JSON.parse(line), { jsonrpc: "2.0", id: 7, method: "keyring.retry" });
});

test("Retry asks the Daemon for a keyring problem and fetches otherwise", () => {
  assert.equal(State.retryKind({ screen: "keyring-unavailable" }), "keyring");
  assert.equal(State.retryKind(State.downloadFailed(22)), "download");
});
