.pragma library

.import "ChatList.js" as ChatList
.import "Common.js" as Common
.import "Draft.js" as Draft
.import "Media.js" as Media
.import "NewChat.js" as NewChat
.import "Timeline.js" as Timeline

// Pure Plugin state logic: Daemon frames and process events in, view state out.
// No QML imports, so `node --test` can load it (see tests/load.js). It imports only the sibling modules in state/.
// This module sits at the top of the import graph: nothing imports it.
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

// The notice for each error code that a request can get, from docs/contract.md. The view shows a notice for it.
// The Plugin depends on the code, never on the message text of the Daemon.
var ERROR_NOTICES = {
  "-32007": Common.NO_WHATSAPP,
  "-32010": "That chat is not in your chat list",
  "-32011": "You can pin up to 3 chats",
  "-32012": "30 files at most",
  "-32013": "That file cannot be sent"
}

// The screens of the pop-out.
var SCREEN = {
  STARTING: "starting",
  ERROR: "error",
  READY: "ready",
  KEYRING_UNAVAILABLE: "keyring-unavailable",
  LINKING: "linking",
  LINKED: "linked"
}

function error(reason, message) {
  return { screen: SCREEN.ERROR, reason: reason, message: message }
}

function initialState() {
  return { screen: SCREEN.STARTING, reason: "", message: "Starting whisker…" }
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
  return { action: "download", view: { screen: SCREEN.STARTING, reason: "", message: "Downloading the Daemon…" } }
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
// `keyringPlain` comes with `ready` and holds until the next one.
function applyFrame(state, frame) {
  var next = foldFrame(state, frame)
  var keep = frame && frame.method === "ready" ? ["settings"] : ["settings", "keyringPlain"]
  for (var i = 0; i < keep.length; i++) {
    if (state[keep[i]] && !next[keep[i]]) {
      if (next === state) return state
      next[keep[i]] = state[keep[i]]
    }
  }
  return next
}

// The Settings notice when the keyring that holds the store key has no password. Empty when there is nothing to say.
var KEYRING_PLAIN_NOTICE = "Your keyring has no password. Anyone with a copy of your files can read your chats. See “Your data” in the README."
function keyringNotice(state) {
  return state && state.keyringPlain === true ? KEYRING_PLAIN_NOTICE : ""
}

// What each Daemon notification does to the Plugin. One row per method of docs/contract.md; the helpers below read this table.
//   fold(state, params): the new view state.
//   refreshesChats, requestsSettings, resendsAttention: the Plugin sends that request after the frame.
// A notification with no row changes nothing.
var NOTIFICATIONS = {
  ready: {
    fold: function (state, params) { return { screen: SCREEN.READY, reason: "", message: "The Daemon is ready.", canRetry: false, keyringPlain: !!(params && params.keyringPlain) } },
    requestsSettings: true,
    resendsAttention: true
  },
  keyringUnavailable: {
    fold: function (state) {
      return {
        screen: SCREEN.KEYRING_UNAVAILABLE,
        reason: "keyring-unavailable",
        message: "The keyring is unavailable. Unlock the keyring, then select Retry.",
        canRetry: true
      }
    }
  },
  linkQr: {
    fold: function (state, params) {
      return linking(state, { qr: params.qr, reason: "", message: LINK_QR_MESSAGE, canRetry: false })
    }
  },
  linked: {
    fold: function (state) { return { screen: SCREEN.LINKED, reason: "", message: "whisker is linked.", canRetry: false, connection: "connecting" } },
    refreshesChats: true
  },
  unlinked: {
    fold: function (state, params) {
      if (params && params.by === "user")
        return { screen: SCREEN.LINKING, reason: "logged-out", message: "You logged out. Link whisker again.", qr: "", pairCode: "", canRetry: false }
      return { screen: SCREEN.LINKING, reason: "unlinked", message: "Your phone removed this device. Link whisker again.", qr: "", pairCode: "", canRetry: false }
    }
  },
  linkFailed: {
    fold: function (state, params) {
      return linking(state, { qr: "", reason: params.reason, message: LINK_FAILURES[params.reason] || LINK_FAILURES.unexpected, canRetry: true })
    }
  },
  chatsChanged: { refreshesChats: true },
  connected: { fold: function (state) { return Common.withChanges(state, { connection: "online" }) } },
  disconnected: { fold: function (state) { return Common.withChanges(state, { connection: "offline", activity: {} }) } },
  messageExpired: { fold: Timeline.removeMessage },
  chatCleared: { fold: Timeline.clearMessages, refreshesChats: true },
  chatRemoved: { fold: ChatList.chatRemoved, refreshesChats: true },
  historyReceived: { fold: Timeline.historyReceived },
  messageChanged: { fold: function (state, params) { return Timeline.upsertMessage(state, params.message) } },
  chatMerged: { fold: Timeline.chatMerged },
  presenceChanged: { fold: ChatList.presenceChanged },
  pictureChanged: { fold: ChatList.pictureChanged },
  activityChanged: { fold: ChatList.activityChanged }
}

// True when the frame removes the open Chat (chatRemoved). Call it before the fold, to end what belongs to the open Chat.
function removesOpenChat(state, frame) {
  return !!frame && frame.method === "chatRemoved" && ChatList.removesOpenChat(state, frame.params)
}

// The row of a notification, or null for a response and for a method with no row.
function effectsOf(frame) {
  if (!frame || !Object.prototype.hasOwnProperty.call(NOTIFICATIONS, frame.method)) return null
  return NOTIFICATIONS[frame.method]
}

function foldFrame(state, frame) {
  if (state.sending && frame && frame.id === state.sending.id) state = Draft.sendAnswered(state, frame)
  var effects = effectsOf(frame)
  if (effects && effects.fold) return effects.fold(state, frame.params)
  var chatState = foldChatResult(state, frame)
  if (chatState) return chatState
  if (frame && frame.result && frame.result.readReceipts !== undefined) {
    var withSettings = {}
    for (var key in state) withSettings[key] = state[key]
    withSettings.settings = frame.result
    return withSettings
  }
  if (frame && frame.result && frame.result.chats)
    return ChatList.chatListPage(state, frame.result)
  if (frame && frame.result && frame.result.code)
    return linking(state, { pairCode: frame.result.code, message: "Enter this code in WhatsApp: Linked devices, Link a device, Link with phone number instead." })
  if (frame && frame.error && PAIR_CODE_ERRORS[frame.error.code])
    return linking(state, { message: PAIR_CODE_ERRORS[frame.error.code] })
  if (frame && frame.error && ERROR_NOTICES[frame.error.code])
    return Timeline.stopWaitingForPhone(Common.withChanges(state, NewChat.markLookupOffline(state, frame.error.code, { notice: ERROR_NOTICES[frame.error.code] })), frame.error.code)
  return state
}

// The state without the notice. The pop-out calls it when the notice has shown long enough.
function clearNotice(state) {
  return Common.withChanges(state, { notice: "" })
}

// True when the Plugin must ask for the first page of the Chat list after this frame.
// A refresh shows page 0 only: a Chat list the user scrolled further falls back to it.
function refreshesChats(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.refreshesChats === true
}

// The linking screen keeps the QR code and the Pair Code unless a frame changes them.
function linking(state, changes) {
  var next = { screen: SCREEN.LINKING, reason: "", message: "", qr: state.qr || "", pairCode: state.pairCode || "", canRetry: false }
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
  return Common.request(id, "keyring.retry")
}

// The request that links by Pair Code. `phone` is what the user typed: only the digits count.
function pairCodeRequest(id, phone) {
  var digits = String(phone).replace(/\D/g, "")
  return Common.request(id, "link.pairCode", { phone: digits })
}

// The request that restarts linking after a failure.
function linkRetryRequest(id) {
  return Common.request(id, "link.retry")
}

// What the Retry button does in this view state: "keyring" and "link" ask the Daemon, "download" fetches the Daemon again.
function retryKind(state) {
  if (state.screen === SCREEN.KEYRING_UNAVAILABLE) return "keyring"
  return state.screen === SCREEN.LINKING ? "link" : "download"
}

// The Read Receipt modes in the order the settings view lists them. Mirror of daemon/internal/settings.
var READ_RECEIPT_MODES = [
  { value: "off", label: "Off", description: "Senders never see that you read their Messages." },
  { value: "onOpen", label: "On open", description: "Senders see it when you open their Chat." },
  { value: "onInteraction", label: "On interaction", description: "Senders see it only when you reply or react." }
]

// The size limits of "Download media". Mirror of the AutoDownloadLimit constants in daemon/internal/settings.
var AUTO_DOWNLOAD_LIMITS = [
  { value: "1mb", label: "1 MB" },
  { value: "5mb", label: "5 MB" },
  { value: "25mb", label: "25 MB" },
  { value: "any", label: "No limit" }
]

// The on/off Profile Settings, by wire name, with the texts of their switches. Mirror of daemon/internal/settings.
var SETTING_TOGGLES = {
  sendOnline: { label: "Send online", description: "Contacts see when you are online." },
  sendTyping: { label: "Send typing", description: "The Chat shows \"typing…\" while you write." },
  notifications: { label: "Notifications", description: "Show new Messages when the pop-out is closed." },
  notificationPreview: { label: "Show preview", description: "Show the sender and text in a notification." },
  autoDownload: { label: "Download media", description: "Images, stickers and voice notes download by themselves." }
}

// True when the Plugin must read the Profile Settings after this frame.
function requestsSettings(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.requestsSettings === true
}

function settingsGetRequest(id) {
  return Common.request(id, "settings.get")
}

// `changes` holds only the settings that change, for example { sendTyping: false }.
function settingsSetRequest(id, changes) {
  return Common.request(id, "settings.set", changes)
}

// The request for Log out. The Plugin sends it only after the user confirmed.
function logoutRequest(id) {
  return Common.request(id, "profile.logout")
}

// Responses about the open Chat. Returns null when the frame is about something else.
function foldChatResult(state, frame) {
  if (!frame) return null
  if (frame.result && frame.result.draft) return Draft.draftLoaded(state, frame.result)
  if (frame.result && frame.result.phone !== undefined) return NewChat.lookupAnswered(state, frame.result)
  if (frame.result && frame.result.contacts) return Common.withChanges(state, { contacts: frame.result.contacts })
  if (frame.result && frame.result.path) return Common.withChanges(state, { mediaPaths: Common.withChanges(state.mediaPaths || {}, Media.mediaPathEntry(frame.result)) })
  if (frame.result && frame.result.data !== undefined && frame.result.chat) return ChatList.pictureLoaded(state, frame.result)
  if (frame.result && frame.result.messages) return Timeline.messagesPage(state, frame.result)
  if (frame.result && frame.result.message) return Timeline.upsertMessage(state, frame.result.message)
  if (frame.result && frame.result.sent) return frame.result.sent.reduce(Timeline.upsertMessage, state)
  return null
}

// "Connecting…" or "Offline" while the connection is not up, otherwise "".
function connectionBanner(state) {
  if (state.connection === "connecting") return "Connecting…"
  return state.connection === "offline" ? "Offline" : ""
}

// The bar icon dims unless the Plugin is ready or linked, and while offline.
function barDimmed(state) {
  if (state.screen !== SCREEN.READY && state.screen !== SCREEN.LINKED) return true
  return state.connection === "offline"
}

// Whether the Plugin sends draft.set for the Draft of the view now. `state` is the state before `frame`; leave `frame` out for a change of the Draft.
// While a send is pending the Daemon still holds the sent Draft, and a draft.set would drop its Attachments. So the Plugin waits.
// The answer to the send then triggers one draft.set: after an error it stores the restored Draft (typed text and sent Attachments);
// after a success the Daemon cleared the Draft, so it stores only the typed text, and nothing when there is none.
function draftSetDue(state, frame) {
  if (!frame) return !state.sending
  if (!state.sending || frame.id !== state.sending.id) return false
  if (frame.error) return true
  var draft = Draft.draftOf(applyFrame(state, frame))
  return draft.text !== "" || draft.quote !== "" || draft.edit !== "" || draft.attachments.length > 0
}

// The fixed shortcuts of the pop-out. `shortcut` is "Escape" or "Ctrl+F"; `local` is { showSettings } as in panelParts.
// Escape goes back one level: Chat, then settings, then it closes the pop-out. Ctrl+F searches, on the Chat list only.
// Returns "closeChat", "closeSettings", "closePopout", "focusSearch" or "none".
function shortcutAction(shortcut, state, local) {
  if (shortcut === "Escape") {
    if (state.openChat) return "closeChat"
    if (local.showNewChat) return "closeNewChat"
    return local.showSettings ? "closeSettings" : "closePopout"
  }
  var onList = panelParts(state, { pairMode: false, showSettings: local.showSettings, showNewChat: local.showNewChat }).chatList
  return shortcut === "Ctrl+F" && onList ? "focusSearch" : "none"
}

// True when the user can see the pop-out: it is open, the session is not idle and the screen is not locked.
function attentionOpen(opened, idle, locked) {
  return opened && !idle && !locked
}

// The request that tells the Daemon about the pop-out: `open`, and the Chat it shows (`jid`, or "" for the Chat list).
// The Daemon derives the Attended Chat from it. Send it on every change and again on `ready`.
function attentionRequest(id, open, jid) {
  return Common.request(id, "attention.set", { open: open, chat: jid || "" })
}

// The request for a click on a notification. `token` is the opaque word of the click command, never a Chat id.
function notificationOpenRequest(id, token) {
  return Common.request(id, "notification.open", { token: token })
}

// What a frame means for the click that waits for the answer `pendingId`. Returns null for any other frame,
// { action: "openChat", chat } for the Chat of the token, or { action: "showList" } for an error (an unknown token).
function notificationOpened(pendingId, frame) {
  if (!pendingId || !frame || frame.id !== pendingId) return null
  if (frame.result && frame.result.chat) return { action: "openChat", chat: frame.result.chat }
  return { action: "showList" }
}

// True when the Plugin must send the pop-out state again after this frame: a new Daemon starts with a closed pop-out.
function resendsAttention(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.resendsAttention === true
}

// The number of unread Chats the bar shows. The Daemon counts them over the whole chat store, without Muted Chats.
function unreadCount(state) {
  return state.unreadChats || 0
}

// The bar label: the count, or "" so that only the icon shows when no Chat is unread.
function unreadLabel(state) {
  var count = unreadCount(state)
  return count > 0 ? String(count) : ""
}

// The header text after the name: " · 3 unread", or "" when no Chat is unread.
function unreadSuffix(state) {
  var count = unreadCount(state)
  return count > 0 ? " · " + count + " unread" : ""
}

// True when contacts see the user Online: connected, and "send online" is on. The Daemon sends Online while the pop-out is open.
function ownOnline(state) {
  return state.connection === "online" && !!state.settings && state.settings.sendOnline === true
}

// The status line shows the state text. A linked whisker has nothing to say; the header shows it.
function statusLineShown(state) {
  return state.screen !== SCREEN.LINKED
}

var LINK_QR_MESSAGE = "Scan this QR code in WhatsApp: Linked devices, Link a device."
var LINK_PAIR_PROMPT = "Type the phone number of your WhatsApp account, with the country code. Then press Enter."

// The line above the QR code or the Pair Code form. The QR text never shows in pair mode, nor the Pair Code text in QR mode.
function linkMessage(state, pairMode) {
  if (state.screen !== SCREEN.LINKING) return state.message
  if (!pairMode) return state.qr ? LINK_QR_MESSAGE : state.message
  if (state.pairCode || (state.message && state.message !== LINK_QR_MESSAGE)) return state.message
  return LINK_PAIR_PROMPT
}

// Which parts of the pop-out show. `local` is the view state that only the pop-out keeps: { pairMode, showSettings }.
// The views read these flags and hold no screen checks of their own.
function panelParts(state, local) {
  var linked = state.screen === SCREEN.LINKED
  var linking = state.screen === SCREEN.LINKING
  var chat = !!state.openChat
  return {
    error: state.screen === SCREEN.ERROR,
    banner: linked && connectionBanner(state) !== "",
    qr: linking && !!state.qr && !local.pairMode,
    pairForm: linking && local.pairMode,
    pairCode: linking && !!local.pairMode && !!state.pairCode,
    pairToggle: linking,
    chatList: linked && !local.showSettings && !local.showNewChat && !chat,
    loadMoreChats: linked && state.moreChats === true && !local.showSettings && !local.showNewChat && !chat,
    newChat: linked && !!local.showNewChat && !local.showSettings && !chat,
    newChatButton: linked && !local.showSettings && !local.showNewChat && !chat,
    settingsButton: linked && !!state.settings && !chat,
    settings: linked && local.showSettings && !!state.settings,
    chat: linked && !local.showSettings && chat,
    emptyChat: linked && !local.showSettings && chat && state.loadedChat === state.openChat.jid && state.openChat.messages.length === 0
  }
}

// The line under the Read Receipts control: what the set mode does. Empty before the settings arrive.
function readReceiptDescription(settings) {
  if (!settings) return ""
  for (var i = 0; i < READ_RECEIPT_MODES.length; i++)
    if (READ_RECEIPT_MODES[i].value === settings.readReceipts) return READ_RECEIPT_MODES[i].description
  return ""
}
