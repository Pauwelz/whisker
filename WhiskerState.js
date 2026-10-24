.pragma library

// Pure Plugin state logic: Daemon frames and process events in, view state out.
// No QML imports, so `node --test` can load it (see tests/load.js).
// A view state is { screen, reason, message }. Views only show it.

// Mirror of the exit code table in docs/contract.md and daemon/internal/contract.
var EXIT_OVERSIZED_LINE = 10

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
