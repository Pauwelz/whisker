.pragma library

// Pure Plugin state logic: Daemon frames and process events in, view state out.
// No QML imports, so `node --test` can load it (see tests/load.js).
// A view state is { screen, reason, message }. Views only show it.

// Mirror of the exit code table in docs/contract.md and daemon/internal/contract.
var EXIT_OVERSIZED_LINE = 10

// Mirror of the exit code table in docs/releasing.md and scripts/fetch-daemon.sh.
var FETCH_EXIT_MESSAGES = {
  20: "The Plugin manifest has no version.",
  21: "SHA256SUMS has no entry for this version.",
  22: "The download failed. Check the network.",
  23: "The checksum of the download did not match."
}

function error(reason, message) {
  return { screen: "error", reason: reason, message: message }
}

function initialState() {
  return { screen: "starting", reason: "", message: "Starting whisker…" }
}

// Returns an error state when the Daemon binary must not start, otherwise null.
// `output` is what `whisker-daemon --version` printed. A missing or broken binary prints nothing.
function checkVersion(expected, output) {
  var actual = String(output).trim()
  if (actual === "") return error("binary-missing", "The whisker Daemon is missing. Run scripts/build-daemon.sh.")
  if (actual !== expected)
    return error("version-mismatch", "The Daemon version (" + actual + ") does not match the Plugin version (" + expected + ").")
  return null
}

// Decides what the Plugin does with the `--version` output. `downloaded` is true after one fetch.
// action: "start" | "download" | "fail". A wrong binary is replaced once, never run.
function versionStep(expected, output, downloaded) {
  var failure = checkVersion(expected, output)
  if (!failure) return { action: "start", view: initialState() }
  if (downloaded) return { action: "fail", view: failure }
  return { action: "download", view: { screen: "starting", reason: "", message: "Downloading the Daemon…" } }
}

function downloadFailed(exitCode) {
  var detail = FETCH_EXIT_MESSAGES[exitCode] || "The fetch script stopped (exit code " + exitCode + ")."
  var state = error("download-failed", "Daemon download failed. " + detail)
  state.canRetry = true
  return state
}

function parseLine(line) {
  try {
    return JSON.parse(line)
  } catch (e) {
    return null
  }
}

function applyFrame(state, frame) {
  if (frame && frame.method === "ready") return { screen: "ready", reason: "", message: "The Daemon is ready." }
  return state
}

function daemonExited(state, exitCode) {
  if (exitCode === EXIT_OVERSIZED_LINE)
    return error("oversized-line", "The Daemon stopped because it got a line that was too long.")
  return error("daemon-exited", "The Daemon stopped (exit code " + exitCode + ").")
}
