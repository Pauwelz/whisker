.pragma library

// Pure Plugin state logic: Daemon frames and process events in, view state out.
// No QML imports, so `node --test` can load it (see tests/load.js).
// A view state is { screen, reason, message, canRetry }. Views only show it.

// Mirror of the exit code table in docs/contract.md and daemon/internal/contract.
var EXIT_OVERSIZED_LINE = 10

var EXIT_PROFILE_IN_USE = 11

// Mirror of the exit code table in docs/releasing.md and scripts/fetch-daemon.sh.
var FETCH_EXIT_MESSAGES = {
  20: "The Plugin manifest has no version.",
  21: "SHA256SUMS has no entry for this version.",
  22: "The download failed. Check the network.",
  23: "The checksum of the download did not match."
}

// Mirror of the linkFailed reasons in docs/contract.md and daemon/internal/whatsapp.
var LINK_FAILURES = {
  "timeout": "The QR code expired. Select Retry for a new one.",
  "client-outdated": "WhatsApp rejects this version of whisker. Update whisker.",
  "scanned-without-multidevice": "Your phone scanned the code, but multi-device is off. Turn it on, then select Retry.",
  "pair-error": "Linking failed on the phone. Select Retry.",
  "connect-failed": "whisker could not reach WhatsApp. Check the network, then select Retry.",
  "unexpected": "Linking failed. Select Retry."
}

// Mirror of the error codes of link.pairCode in docs/contract.md.
var PAIR_CODE_ERRORS = {
  "-32001": "Enter the phone number with its country code, digits only.",
  "-32002": "WhatsApp gave no Pair Code. Try again."
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

// Profile Settings live on disk, so every frame keeps them, including the ones that reset the rest.
function applyFrame(state, frame) {
  var next = foldFrame(state, frame)
  if (state.settings && !next.settings) {
    if (next === state) return state
    next.settings = state.settings
  }
  return next
}

function foldFrame(state, frame) {
  if (frame && frame.result && frame.result.readReceipts !== undefined) {
    var withSettings = {}
    for (var key in state) withSettings[key] = state[key]
    withSettings.settings = frame.result
    return withSettings
  }
  if (frame && frame.method === "ready")
    return { screen: "ready", reason: "", message: "The Daemon is ready.", canRetry: false }
  if (frame && frame.method === "keyringUnavailable")
    return {
      screen: "keyring-unavailable",
      reason: "keyring-unavailable",
      message: "The keyring is unavailable. Unlock the keyring, then select Retry.",
      canRetry: true
    }
  if (frame && frame.method === "linkQr")
    return linking(state, { qr: frame.params.qr, reason: "", message: "Scan this QR code in WhatsApp: Linked devices, Link a device.", canRetry: false })
  if (frame && frame.method === "linked")
    return { screen: "linked", reason: "", message: "whisker is linked.", canRetry: false }
  if (frame && frame.method === "unlinked" && frame.params && frame.params.by === "user")
    return { screen: "linking", reason: "logged-out", message: "You logged out. Link whisker again.", qr: "", pairCode: "", canRetry: false }
  if (frame && frame.method === "unlinked")
    return { screen: "linking", reason: "unlinked", message: "Your phone removed this device. Link whisker again.", qr: "", pairCode: "", canRetry: false }
  if (frame && frame.method === "linkFailed") {
    var reason = frame.params.reason
    return linking(state, { qr: "", reason: reason, message: LINK_FAILURES[reason] || LINK_FAILURES.unexpected, canRetry: true })
  }
  if (frame && frame.result && frame.result.chats)
    return chatListPage(state, frame.result)
  if (frame && frame.result && frame.result.code)
    return linking(state, { pairCode: frame.result.code, message: "Enter this code in WhatsApp: Linked devices, Link with phone number." })
  if (frame && frame.error && PAIR_CODE_ERRORS[frame.error.code])
    return linking(state, { message: PAIR_CODE_ERRORS[frame.error.code] })
  return state
}

// A chats.list result. Page 0 replaces the Chat list, a later page extends it.
function chatListPage(state, result) {
  var next = {}
  for (var key in state) next[key] = state[key]
  next.chats = result.offset > 0 ? (state.chats || []).concat(result.chats) : result.chats
  next.moreChats = result.more === true
  return next
}

// True when the Plugin must ask for the first page of the Chat list after this frame.
// A refresh shows page 0 only: a Chat list the user scrolled further falls back to it.
function refreshesChats(frame) {
  return !!frame && (frame.method === "linked" || frame.method === "chatsChanged")
}

// The request for one page of the Chat list. The next page starts at the number of Chats shown.
function chatListRequest(id, offset) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "chats.list", params: { offset: offset } }) + "\n"
}

// The linking screen keeps the QR code and the Pair Code unless a frame changes them.
function linking(state, changes) {
  var next = { screen: "linking", reason: "", message: "", qr: state.qr || "", pairCode: state.pairCode || "", canRetry: false }
  for (var key in changes) next[key] = changes[key]
  return next
}

function daemonExited(state, exitCode) {
  if (exitCode === EXIT_OVERSIZED_LINE)
    return error("oversized-line", "The Daemon stopped because it got a line that was too long.")
  if (exitCode === EXIT_PROFILE_IN_USE)
    return error("profile-in-use", "Another whisker Daemon uses this Profile. Close it and restart the shell.")
  return error("daemon-exited", "The Daemon stopped (exit code " + exitCode + ").")
}

// The request that asks the Daemon to try the keyring again. Send it as one line.
function keyringRetryRequest(id) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "keyring.retry" }) + "\n"
}

// The request that links by Pair Code. `phone` is what the user typed: only the digits count.
function pairCodeRequest(id, phone) {
  var digits = String(phone).replace(/\D/g, "")
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "link.pairCode", params: { phone: digits } }) + "\n"
}

// The request that restarts linking after a failure.
function linkRetryRequest(id) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "link.retry" }) + "\n"
}

// What the Retry button does in this view state: "keyring" and "link" ask the Daemon, "download" fetches the Daemon again.
function retryKind(state) {
  if (state.screen === "keyring-unavailable") return "keyring"
  return state.screen === "linking" ? "link" : "download"
}

// The Read Receipt modes in the order the settings view lists them. Mirror of daemon/internal/settings.
var READ_RECEIPT_MODES = [
  { value: "off", label: "Off" },
  { value: "onOpen", label: "On open" },
  { value: "onInteraction", label: "On interaction" }
]

// True when the Plugin must read the Profile Settings after this frame.
function requestsSettings(frame) {
  return !!frame && frame.method === "ready"
}

function settingsGetRequest(id) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "settings.get" }) + "\n"
}

// `changes` holds only the settings that change, for example { sendTyping: false }.
function settingsSetRequest(id, changes) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "settings.set", params: changes }) + "\n"
}

// The request for Log out. The Plugin sends it only after the user confirmed.
function logoutRequest(id) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: "profile.logout" }) + "\n"
}
