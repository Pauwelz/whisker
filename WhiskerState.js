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

// The notice for each error code that a request can get, from docs/contract.md. The view shows a notice for it.
// The Plugin depends on the code, never on the message text of the Daemon.
var NO_WHATSAPP = "Could not reach WhatsApp"

var ERROR_NOTICES = {
  "-32007": NO_WHATSAPP,
  "-32010": "That chat is not in your chat list",
  "-32011": "You can pin up to 3 chats",
  "-32012": "30 files at most",
  "-32013": "That file cannot be sent"
}

// The screens of the pop-out, and the states of Media. The Daemon sends the Media states (docs/contract.md).
var SCREEN = {
  STARTING: "starting",
  ERROR: "error",
  READY: "ready",
  KEYRING_UNAVAILABLE: "keyring-unavailable",
  LINKING: "linking",
  LINKED: "linked"
}

var MEDIA_STATE = { NONE: "none", DOWNLOADING: "downloading", READY: "ready", FAILED: "failed" }

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
function applyFrame(state, frame) {
  var next = foldFrame(state, frame)
  if (state.settings && !next.settings) {
    if (next === state) return state
    next.settings = state.settings
  }
  return next
}

// What each Daemon notification does to the Plugin. One row per method of docs/contract.md; the helpers below read this table.
//   fold(state, params): the new view state.
//   refreshesChats, requestsSettings, resendsAttention: the Plugin sends that request after the frame.
// A notification with no row changes nothing.
var NOTIFICATIONS = {
  ready: {
    fold: function (state) { return { screen: SCREEN.READY, reason: "", message: "The Daemon is ready.", canRetry: false } },
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
  connected: { fold: function (state) { return withChanges(state, { connection: "online" }) } },
  disconnected: { fold: function (state) { return withChanges(state, { connection: "offline", activity: {} }) } },
  messageExpired: { fold: removeMessage },
  historyReceived: { fold: historyReceived },
  messageChanged: { fold: function (state, params) { return upsertMessage(state, params.message) } },
  chatMerged: { fold: chatMerged },
  presenceChanged: { fold: presenceChanged },
  pictureChanged: { fold: pictureChanged },
  activityChanged: { fold: activityChanged }
}

// The row of a notification, or null for a response and for a method with no row.
function effectsOf(frame) {
  if (!frame || !Object.prototype.hasOwnProperty.call(NOTIFICATIONS, frame.method)) return null
  return NOTIFICATIONS[frame.method]
}

function foldFrame(state, frame) {
  if (state.sending && frame && frame.id === state.sending.id) state = sendAnswered(state, frame)
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
    return chatListPage(state, frame.result)
  if (frame && frame.result && frame.result.code)
    return linking(state, { pairCode: frame.result.code, message: "Enter this code in WhatsApp: Linked devices, Link a device, Link with phone number instead." })
  if (frame && frame.error && PAIR_CODE_ERRORS[frame.error.code])
    return linking(state, { message: PAIR_CODE_ERRORS[frame.error.code] })
  if (frame && frame.error && ERROR_NOTICES[frame.error.code])
    return stopWaitingForPhone(withChanges(state, markLookupOffline(state, frame.error.code, { notice: ERROR_NOTICES[frame.error.code] })), frame.error.code)
  return state
}

// The state without the notice. The pop-out calls it when the notice has shown long enough.
function clearNotice(state) {
  return withChanges(state, { notice: "" })
}

// A chats.list result. Page 0 replaces the Chat list, a later page extends it.
function chatListPage(state, result) {
  var next = {}
  for (var key in state) next[key] = state[key]
  next.chats = result.offset > 0 ? (state.chats || []).concat(result.chats) : result.chats
  next.moreChats = result.more === true
  next.unreadChats = result.unreadChats || 0
  return next
}

// True when the Plugin must ask for the first page of the Chat list after this frame.
// A refresh shows page 0 only: a Chat list the user scrolled further falls back to it.
function refreshesChats(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.refreshesChats === true
}

// The request for one page of the Chat list. The next page starts at the number of Chats shown.
// A non-empty query asks for the Chats that match the search field.
// archived asks for the archived Chats only. Without it, the list hides them.
function chatListRequest(id, offset, query, archived) {
  var params = { offset: offset }
  if (query) params.query = query
  if (archived) params.archived = true
  return request(id, "chats.list", params)
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
  return request(id, "keyring.retry")
}

// The request that links by Pair Code. `phone` is what the user typed: only the digits count.
function pairCodeRequest(id, phone) {
  var digits = String(phone).replace(/\D/g, "")
  return request(id, "link.pairCode", { phone: digits })
}

// The request that restarts linking after a failure.
function linkRetryRequest(id) {
  return request(id, "link.retry")
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
  autoDownload: { label: "Download media", description: "Images, stickers and voice notes download by themselves." }
}

// True when the Plugin must read the Profile Settings after this frame.
function requestsSettings(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.requestsSettings === true
}

function settingsGetRequest(id) {
  return request(id, "settings.get")
}

// `changes` holds only the settings that change, for example { sendTyping: false }.
function settingsSetRequest(id, changes) {
  return request(id, "settings.set", changes)
}

// The request for Log out. The Plugin sends it only after the user confirmed.
function logoutRequest(id) {
  return request(id, "profile.logout")
}

// One JSON-RPC request line for the Daemon. `params` is left out when undefined.
function request(id, method, params) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: method, params: params }) + "\n"
}

// Qt key codes of the two Enter keys. The composer sends the code of the key that was pressed.
var KEY_RETURN = 0x01000004

var KEY_ENTER = 0x01000005

// Qt key code of V: Ctrl+V pastes in the composer.
var KEY_V = 0x56

function withChanges(state, changes) {
  var next = {}
  for (var key in state) next[key] = state[key]
  for (var change in changes) next[change] = changes[change]
  return next
}

// Responses about the open Chat. Returns null when the frame is about something else.
function foldChatResult(state, frame) {
  if (!frame) return null
  if (frame.result && frame.result.draft) return draftLoaded(state, frame.result)
  if (frame.result && frame.result.phone !== undefined) return lookupAnswered(state, frame.result)
  if (frame.result && frame.result.contacts) return withChanges(state, { contacts: frame.result.contacts })
  if (frame.result && frame.result.path) return withChanges(state, { mediaPaths: withChanges(state.mediaPaths || {}, mediaPathEntry(frame.result)) })
  if (frame.result && frame.result.data !== undefined && frame.result.chat) return pictureLoaded(state, frame.result)
  if (frame.result && frame.result.messages) return messagesPage(state, frame.result)
  if (frame.result && frame.result.message) return upsertMessage(state, frame.result.message)
  if (frame.result && frame.result.sent) return frame.result.sent.reduce(upsertMessage, state)
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

// Opens a Chat of the Chat list. `chat` is a Chat of the list: jid, name, kind. Messages arrive with chat.messages.
function openChat(state, chat) {
  return withChanges(state, { openChat: { jid: chat.jid, name: chat.name, kind: chat.kind, messages: [], moreMessages: false } })
}

function closeChat(state) {
  var next = withChanges(state, {})
  delete next.openChat
  delete next.mediaPaths
  delete next.loadedChat
  return next
}

// Puts a Message in the open Chat: it replaces the Message with that id, or goes after the last Message that is not newer.
function upsertMessage(state, message) {
  var chat = state.openChat
  if (!chat || chat.jid !== message.chat) return state
  var messages = chat.messages.slice()
  for (var i = 0; i < messages.length; i++) {
    if (messages[i].id === message.id) {
      messages[i] = message
      return withChanges(state, { openChat: withChanges(chat, { messages: messages }) })
    }
  }
  var at = messages.length
  while (at > 0 && messages[at - 1].time > message.time) at--
  messages.splice(at, 0, message)
  return withChanges(state, { openChat: withChanges(chat, { messages: messages }) })
}

// Two Chats became one. The open Chat takes the JID of the merged Chat and shows no Messages until they load again.
function chatMerged(state, merged) {
  var chat = state.openChat
  if (!chat || (chat.jid !== merged.from && chat.jid !== merged.to)) return state
  return withChanges(state, { openChat: withChanges(chat, { jid: merged.to, messages: [], moreMessages: false }) })
}

// True when the Plugin must load the Messages and the Draft of the open Chat again after this frame. Call it with the state after the frame.
function reloadsOpenChat(state, frame) {
  return !!frame && frame.method === "chatMerged" && !!state.openChat && state.openChat.jid === frame.params.to
}

// On-Demand History Sync. The open Chat asks the phone for older Messages when no stored Message is left.
// openChat.historyLoading is set while the request is open. state.nothingMore (JID to true) lives in Plugin memory only.
var OLDER_LOAD = "load older"
var OLDER_LOADING = "loading from phone…"
var NO_ANSWER = "No older Messages came from your phone."

// The text of the older link, or "" while it is hidden. It waits for the first page of the Chat.
function olderLink(state) {
  var chat = state.openChat
  if (!chat || state.loadedChat !== chat.jid || (state.nothingMore || {})[chat.jid]) return ""
  return chat.historyLoading ? OLDER_LOADING : OLDER_LOAD
}

// What a tap on the older link does: "messages" (chat.messages), "history" (chat.requestHistory) or "" (nothing).
function olderAction(state) {
  if (olderLink(state) !== OLDER_LOAD) return ""
  return state.openChat.moreMessages ? "messages" : "history"
}

function historyRequested(state) {
  return withChanges(state, { openChat: withChanges(state.openChat, { historyLoading: true }) })
}

function historyRequest(id, jid) {
  return request(id, "chat.requestHistory", { chat: jid })
}

function historyReceived(state, params) {
  var chat = state.openChat
  if (!chat || chat.jid !== params.chat) return state
  var next = withChanges(state, { openChat: withChanges(chat, { historyLoading: undefined }) })
  if (params.timedOut) return withChanges(next, { notice: NO_ANSWER })
  if (params.count > 0) return next
  var done = withChanges(state.nothingMore || {}, {})
  done[chat.jid] = true
  return withChanges(next, { nothingMore: done })
}

// True when the Plugin must load the Messages that On-Demand History Sync stored. Call it with the state after the frame.
function reloadsHistory(state, frame) {
  return !!frame && frame.method === "historyReceived" && !frame.params.timedOut && frame.params.count > 0 &&
    !!state.openChat && state.openChat.jid === frame.params.chat
}

// A request that finds no connection gets no historyReceived, so the wait ends here.
function stopWaitingForPhone(state, code) {
  if (String(code) !== "-32007" || !state.openChat) return state
  return withChanges(state, { openChat: withChanges(state.openChat, { historyLoading: undefined }) })
}

// Takes an expired Message out of the open Chat.
function removeMessage(state, expired) {
  var chat = state.openChat
  if (!chat || chat.jid !== expired.chat) return state
  var messages = chat.messages.filter(function (m) { return m.id !== expired.id })
  return withChanges(state, { openChat: withChanges(chat, { messages: messages }) })
}

// A chat.messages result for the open Chat. A page of another Chat is dropped.
function messagesPage(state, result) {
  var chat = state.openChat
  if (!chat || chat.jid !== result.chat) return state
  var next = state
  // shortcut: a Message with the same second as one already shown can land after it, upgrade to a (time, order) key if that shows.
  for (var i = 0; i < result.messages.length; i++) next = upsertMessage(next, result.messages[i])
  return withChanges(next, { loadedChat: result.chat, openChat: withChanges(next.openChat, { moreMessages: result.more === true }) })
}

// The Draft of the open Chat: { text, quote, edit, attachments }. The Daemon stores it.
// `quote` and `edit` are Message ids or "". `attachments` are the absolute paths of the Attachments, in order.
// Attachments never combine with a Quote or an Edit.
// `openChat.unsendable` lists the Attachment paths that the Daemon refused. The marks live in the view only.
var EMPTY_DRAFT = { text: "", quote: "", edit: "", attachments: [] }

function draftOf(state) {
  return (state.openChat && state.openChat.draft) || EMPTY_DRAFT
}

// A draft.get result. The first one for the open Chat sets the Draft and the marks; a Draft that the user already changed stays.
function draftLoaded(state, result) {
  var chat = state.openChat
  if (!chat || chat.jid !== result.chat || chat.draft !== undefined) return state
  var draft = withChanges(result.draft, { attachments: result.draft.attachments || [] })
  return withChanges(state, { openChat: withChanges(chat, { draft: draft, unsendable: result.unsendable || [] }) })
}

function sameList(a, b) {
  return a.length === b.length && a.every(function(x, i) { return x === b[i] })
}

// Changes the Draft of the open Chat. `changes.attachments` is the whole new list. Returns the same state when nothing changes.
// Attachments never combine with an Edit or a Quote. Starting an Edit or a Quote drops them.
// Adding Attachments drops the Quote, and is refused during an Edit.
// A mark goes away with its row.
function changeDraft(state, changes) {
  if (!state.openChat) return state
  var old = draftOf(state)
  var draft = withChanges(old, changes)
  if (draft.attachments.length && (draft.edit || draft.quote)) {
    if (!changes.attachments || !changes.attachments.length) draft = withChanges(draft, { attachments: [] })
    else if (draft.edit) return state
    else draft = withChanges(draft, { quote: "" })
  }
  var marks = (state.openChat.unsendable || []).filter(function(path) { return draft.attachments.indexOf(path) >= 0 })
  if (draft.text === old.text && draft.quote === old.quote && draft.edit === old.edit && sameList(draft.attachments, old.attachments) &&
      marks.length === (state.openChat.unsendable || []).length) return state
  return withChanges(state, { openChat: withChanges(state.openChat, { draft: draft, unsendable: marks }) })
}

function clearDraft(state) {
  return changeDraft(state, EMPTY_DRAFT)
}

// The composer sent the Draft as request `id`: clear the view Draft at once, and keep the sent one in case the Daemon refuses the send.
// Only a send with Attachments is kept, because only that Draft is worth a restore.
function submitDraft(state, id) {
  var draft = draftOf(state)
  var sending = draft.attachments.length ? { id: id, jid: state.openChat.jid, draft: draft } : null
  return withChanges(clearDraft(state), { sending: sending })
}

// The answer to the send of submitDraft. After an error the Daemon still stores the Draft, so the view takes it back:
// the sent Attachments, marked where error.data.paths names them, and the sent text unless the user typed new text since.
function sendAnswered(state, frame) {
  var sent = state.sending
  var next = withChanges(state, { sending: null })
  if (!frame.error || !next.openChat || next.openChat.jid !== sent.jid) return next
  var typed = draftOf(next).text
  next = changeDraft(next, { text: typed !== "" ? typed : sent.draft.text, attachments: sent.draft.attachments })
  var bad = (frame.error.data && frame.error.data.paths) || []
  return withChanges(next, { openChat: withChanges(next.openChat, { unsendable: bad }) })
}

// Whether the Plugin sends draft.set for the Draft of the view now. `state` is the state before `frame`; leave `frame` out for a change of the Draft.
// While a send is pending the Daemon still holds the sent Draft, and a draft.set would drop its Attachments. So the Plugin waits.
// The answer to the send then triggers one draft.set: after an error it stores the restored Draft (typed text and sent Attachments);
// after a success the Daemon cleared the Draft, so it stores only the typed text, and nothing when there is none.
function draftSetDue(state, frame) {
  if (!frame) return !state.sending
  if (!state.sending || frame.id !== state.sending.id) return false
  if (frame.error) return true
  var draft = draftOf(applyFrame(state, frame))
  return draft.text !== "" || draft.quote !== "" || draft.edit !== "" || draft.attachments.length > 0
}

// The Message that the Draft quotes (or edits, with `field` "edit"), if the Plugin has it loaded. Else null.
function draftTarget(state, field) {
  var id = draftOf(state)[field]
  var messages = state.openChat ? state.openChat.messages : []
  return id && messages.filter(function(m) { return m.id === id })[0] || null
}

function draftGetRequest(id, jid) {
  return request(id, "draft.get", { chat: jid })
}

function draftSetRequest(id, jid, draft) {
  return request(id, "draft.set", { chat: jid, text: draft.text, quote: draft.quote, edit: draft.edit, attachments: draft.attachments })
}

// The request for the people the new Chat view lists. The Daemon returns the first 50 that match the query.
function contactsSearchRequest(id, query) {
  return request(id, "contacts.search", { query: query })
}

// The request to store the Chat with a person, before the first Message or Draft.
function chatStartRequest(id, jid) {
  return request(id, "chat.start", { jid: jid })
}

// The request to ask WhatsApp if a phone number is on WhatsApp.
function lookupRequest(id, phone) {
  return request(id, "contacts.lookup", { phone: phone })
}

// The digits of the search text when it looks like a phone number (digits and + space - . ( )), else null.
function phoneDigits(text) {
  var t = String(text || "").trim()
  if (!/^\+?[\d\s().-]+$/.test(t)) return null
  var digits = t.replace(/\D/g, "")
  return digits === "" ? null : digits
}

// Same rule as the Daemon (-32001): more than 6 digits, no leading 0. The Plugin sends a lookup only for these.
function isLookupPhone(digits) {
  return digits.length > 6 && digits.charAt(0) !== "0"
}

// The digits to look up for the search text, or "" when the text is no full phone number. A keystroke starts at most one lookup.
function lookupPhone(text) {
  var digits = phoneDigits(text)
  return digits !== null && isLookupPhone(digits) ? digits : ""
}

// The lookup of `phone` is sent. Its row shows "checking" until the reply for that number comes.
function lookupStarted(state, phone) {
  return withChanges(state, { lookup: { phone: phone, status: "checking", jid: "" } })
}

// The reply names its number. A reply for another number than the one in the row is late and changes nothing.
function lookupAnswered(state, result) {
  if (!state.lookup || state.lookup.phone !== result.phone) return state
  return withChanges(state, { lookup: { phone: result.phone, status: result.jid ? "found" : "none", jid: result.jid } })
}

// An error reply has no number. A "could not reach WhatsApp" while a lookup waits belongs to it.
// shortcut: a late error of an older lookup can mark the new one offline, the next keystroke repeats the lookup.
function markLookupOffline(state, code, changes) {
  if (String(code) !== "-32007" || !state.lookup || state.lookup.status !== "checking") return changes
  changes.lookup = { phone: state.lookup.phone, status: "offline", jid: "" }
  return changes
}

var LOOKUP_LABELS = {
  malformed: "Enter the full number with its country code",
  checking: "checking WhatsApp…",
  found: "on WhatsApp · message",
  none: "not on WhatsApp",
  offline: NO_WHATSAPP
}

var NEW_CHAT_NOTE = "New group chats are created on the phone."
var EMPTY_CHAT_TEXT = "No messages yet. Your first message starts the chat."

// The rows of the new Chat view for a contacts.search result: one row per person in the order of the Daemon
// (Phone Contacts, then Push-Name-only Participants), then the note about Group Chats.
// A person with a Chat shows "open", any other "new".
// When the search text is a phone number, a "phone" row comes first: checking, found (it has the jid to start), none,
// offline or malformed.
function newChatRows(contacts, query, lookup) {
  var rows = []
  var digits = phoneDigits(query)
  if (digits !== null) {
    var status = !isLookupPhone(digits) ? "malformed" : lookup && lookup.phone === digits ? lookup.status : "checking"
    rows.push({ kind: "phone", phone: "+" + digits, status: status, label: LOOKUP_LABELS[status], jid: status === "found" ? lookup.jid : "" })
  }
  rows = rows.concat((contacts || []).map(function (c) {
    return { kind: "contact", jid: c.jid, name: c.name, phone: c.phone, pushNameOnly: c.pushNameOnly, label: c.chat ? "open" : "new" }
  }))
  rows.push({ kind: "note", text: NEW_CHAT_NOTE })
  return rows
}

// The request for one page of the Chat timeline. The next page starts at the number of Messages shown.
function messagesRequest(id, jid, offset) {
  return request(id, "chat.messages", { chat: jid, offset: offset })
}

// `quoteId` is the Message that the new Message quotes. Leave it out for a plain Message.
function sendRequest(id, jid, text, quoteId) {
  var params = { chat: jid, text: text }
  if (quoteId) params.quote = quoteId
  return request(id, "chat.send", params)
}

// Whether a paste can give an Attachment: not during an Edit, which never combines with one.
function canAttach(editingId) {
  return editingId === ""
}

// The request that reads the clipboard for a Ctrl+V in the composer.
function pasteRequest(id, jid) {
  return request(id, "draft.paste", { chat: jid })
}

// What a draft.paste reply gives the open Chat: the pasted paths, [] to paste text, or null for any other frame.
function pastedAttachment(state, frame) {
  if (!frame || !frame.result || !Array.isArray(frame.result.pasted)) return null
  if (!state.openChat || state.openChat.jid !== frame.result.chat) return null
  return frame.result.pasted
}

// The Attachments of the Draft with the pasted paths added at the end. A path that is already in the list stays once.
function appendAttachments(draft, paths) {
  return paths.reduce(function(list, path) { return list.indexOf(path) < 0 ? list.concat([path]) : list }, draft.attachments)
}

// Whether the composer shows the Attachment rows.
function hasAttachment(draft) {
  return draft.attachments.length > 0
}

// The file name of an Attachment, shown in its row.
function attachmentName(path) {
  return path.split("/").pop()
}

// One row per Attachment: its path, its text, and whether it is marked as not sendable.
function attachmentRows(state) {
  var marks = (state.openChat && state.openChat.unsendable) || []
  return draftOf(state).attachments.map(function(path) {
    var bad = marks.indexOf(path) >= 0
    return { path: path, bad: bad, label: (bad ? "Not sendable: " : "Attached: ") + attachmentName(path) }
  })
}

// The request to send the files at `paths` as Media. The first Message has the caption.
function sendMediaRequest(id, jid, paths, caption) {
  return request(id, "chat.sendMedia", { chat: jid, paths: paths, caption: caption })
}

function retryMessageRequest(id, jid, messageId) {
  return request(id, "chat.retry", { chat: jid, id: messageId })
}

// What the composer does with a key press: "send", "swallow" (Enter with nothing to send), "paste" (ask the Daemon)
// or "default" (the text field handles it). Enter sends and Shift+Enter adds a line. These keybindings are fixed.
// An Attachment can go without text. During an Edit, Ctrl+V pastes text in the text field.
function composerAction(key, shift, text, hasAttachment, ctrl, editingId) {
  if (ctrl && key === KEY_V) return canAttach(editingId || "") ? "paste" : "default"
  if (key !== KEY_RETURN && key !== KEY_ENTER) return "default"
  if (shift) return "default"
  return canSend(text, hasAttachment) ? "send" : "swallow"
}

// A send needs text or an attached file. Enter and the send button share this rule.
function canSend(text, hasAttachment) {
  return String(text).trim() !== "" || !!hasAttachment
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

// The phone number of a JID (user[:device]@server) with a leading "+".
function phoneOf(jid) {
  return "+" + String(jid).split("@")[0].split(":")[0]
}

// The sender shown on a line: "You", the name of the Participant, or "~ name +number" when only a Push Name is known.
function senderLabel(message) {
  if (message.fromMe) return "You"
  return message.senderPush ? "~ " + message.senderName + " " + phoneOf(message.sender) : message.senderName
}

// Colours of the senders in a Group Chat. A Participant always gets the same one.
var SENDER_COLORS = ["#7aa2f7", "#9ece6a", "#e0af68", "#bb9af7", "#7dcfff", "#ff9e64", "#f7768e", "#73daca"]

function senderColor(jid) {
  var hash = 0
  for (var i = 0; i < jid.length; i++) hash = (hash * 31 + jid.charCodeAt(i)) | 0
  return SENDER_COLORS[Math.abs(hash) % SENDER_COLORS.length]
}

function pad2(n) {
  return (n < 10 ? "0" : "") + n
}

// "HH:MM" in local time.
function timeLabel(seconds) {
  var d = new Date(seconds * 1000)
  return pad2(d.getHours()) + ":" + pad2(d.getMinutes())
}

// "YYYY-MM-DD" in local time: the label of a day separator.
function dayLabel(seconds) {
  var d = new Date(seconds * 1000)
  return d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate())
}

// The rows of the timeline, oldest first. Messages arrive oldest first.
// A "day" row opens each day. A "system" row is a line that is not a Message (`system: true`).
// A "message" row is `continued` when the row before it is a Message of the same sender: the line then hides time and sender.
function timelineRows(messages) {
  var rows = []
  var day = ""
  messages.forEach(function (message) {
    var label = dayLabel(message.time)
    if (label !== day) {
      day = label
      rows.push({ kind: "day", label: label })
    }
    if (message.system) {
      rows.push({ kind: "system", message: message })
      return
    }
    var previous = rows[rows.length - 1]
    var continued = previous.kind === "message" && previous.message.fromMe === message.fromMe && previous.message.sender === message.sender
    rows.push({ kind: "message", message: message, continued: continued })
  })
  return rows
}

// The index of the row that holds the Message with this id, or -1. A Quote jumps there.
function rowIndexOf(rows, id) {
  for (var i = 0; i < rows.length; i++) if (rows[i].message && rows[i].message.id === id) return i
  return -1
}

// The key of a timeline row: one day row per date, one row per Message id.
function rowKey(row) {
  return row.kind === "day" ? "day " + row.label : "m " + row.message.id
}

// The edits that turn the list of keys before into the list after: removes from the end first, then inserts in order.
// The timeline list applies them in place of a new model, because a new model scrolls it back to the newest row.
// It returns null when kept keys change order: the list then starts again.
function listEdits(before, after) {
  var kept = {}
  after.forEach(function (key) { kept[key] = true })
  var edits = []
  for (var i = before.length - 1; i >= 0; i--) if (!kept[before[i]]) edits.push({ remove: i })
  var left = before.filter(function (key) { return kept[key] })
  var inLeft = {}
  left.forEach(function (key) { inLeft[key] = true })
  var next = 0
  for (var at = 0; at < after.length; at++) {
    if (next < left.length && left[next] === after[at]) { next++; continue }
    if (inLeft[after[at]]) return null
    edits.push({ insert: at, key: after[at] })
  }
  return edits
}

// The Delivery State column of a line: a mark and its tone. Messages of others have no mark.
function deliveryMark(message) {
  if (!message.fromMe) return { text: "", tone: "normal" }
  switch (message.state) {
    case "failed": return { text: "!", tone: "error" }
    case "read": case "played": return { text: "✓✓", tone: "read" }
    case "delivered": return { text: "✓✓", tone: "normal" }
    case "sent": return { text: "✓", tone: "normal" }
    default: return { text: "…", tone: "normal" }
  }
}

// The line below a failed Message.
function failedLine(message) {
  return canRetryMessage(message) ? "not sent · retry" : ""
}

function canRetryMessage(message) {
  return message.fromMe === true && message.state === "failed"
}

// The request that tells the Daemon about the pop-out: `open`, and the Chat it shows (`jid`, or "" for the Chat list).
// The Daemon derives the Attended Chat from it. Send it on every change and again on `ready`.
function attentionRequest(id, open, jid) {
  return request(id, "attention.set", { open: open, chat: jid || "" })
}

// True when the Plugin must send the pop-out state again after this frame: a new Daemon starts with a closed pop-out.
function resendsAttention(frame) {
  var effects = effectsOf(frame)
  return !!effects && effects.resendsAttention === true
}

// The text of a Tombstone, shown at the position of the revoked Message.
var TOMBSTONE_TEXT = "This message was deleted"

// WhatsApp lets the user edit a Message for 15 minutes. The Daemon enforces it; the Plugin only hides the button.
var EDIT_WINDOW_SECONDS = 15 * 60

function messageBody(message) {
  return message.revoked ? TOMBSTONE_TEXT : message.text
}

// The only links the Plugin makes clickable: the map link of a location. Other text stays plain.
var MAP_LINK = /https:\/\/www\.openstreetmap\.org\/\?mlat=-?[0-9.]+&(?:amp;)?mlon=-?[0-9.]+#map=[0-9]+\/-?[0-9.]+\/-?[0-9.]+/g

function escapeHtml(text) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
}

// The Message text as Qt StyledText: mentions in bold, the map link of a location as a link.
function messageHtml(message) {
  var html = escapeHtml(messageBody(message))
  if (!message.revoked) {
    // Longest mention first, so "@Anna Lee" is not cut by "@Anna". Mentions are plain names from the Daemon.
    var mentions = (message.mentions || []).slice().sort(function(a, b) { return b.length - a.length })
    mentions.forEach(function(mention) {
      html = html.split(escapeHtml(mention)).join("<b>" + escapeHtml(mention) + "</b>")
    })
  }
  return html.replace(/\n/g, "<br>")
}

// The map link of a location Message, for its Open map button, or "".
function mapLink(message) {
  if (message.revoked) return ""
  MAP_LINK.lastIndex = 0
  var match = MAP_LINK.exec(message.text || "")
  return match ? match[0] : ""
}

// The Open map button of a Message: { url }, or null when the Message has no map link.
function mapView(message) {
  var url = mapLink(message)
  return url === "" ? null : { url: url }
}

// The command that opens a map link in the browser, or [] for any other link.
// No "--": xdg-open rejects it. A full match starts with "https://", so it cannot pass as an option.
function linkCommand(url) {
  MAP_LINK.lastIndex = 0
  var match = MAP_LINK.exec(url)
  return match && match[0] === url ? ["xdg-open", url] : []
}

function editedLabel(message) {
  return message.edited && !message.revoked ? "(edited)" : ""
}

// A Message of the user that WhatsApp accepted and that is not a Tombstone.
function canRevokeMessage(message) {
  return message.fromMe === true && !message.revoked && ["sent", "delivered", "read", "played"].indexOf(message.state) >= 0
}

function canEditMessage(message, nowSeconds) {
  return canRevokeMessage(message) && nowSeconds - message.time <= EDIT_WINDOW_SECONDS
}

function editRequest(id, jid, messageId, text) {
  return request(id, "chat.edit", { chat: jid, id: messageId, text: text })
}

function revokeRequest(id, jid, messageId) {
  return request(id, "chat.revoke", { chat: jid, id: messageId })
}

function deleteForMeRequest(id, jid, messageId) {
  return request(id, "chat.deleteForMe", { chat: jid, id: messageId })
}

// The request for the composer text: an edit of `editingId`, or a new Message when `editingId` is empty.
// A new Message quotes `quoteId` when it is set.
// Attachments (`attachments`, a list of paths) go as Media with the text as caption. The Draft never holds a Quote with files.
function composerRequest(id, jid, editingId, text, quoteId, attachments) {
  if (editingId) return editRequest(id, jid, editingId, text)
  return attachments && attachments.length ? sendMediaRequest(id, jid, attachments, text) : sendRequest(id, jid, text, quoteId)
}

// The Reaction picker: the six default emoji of WhatsApp. Mirror of `ReactionEmoji` in daemon/internal/contract/react.go.
var REACTION_EMOJI = ["👍", "❤️", "😂", "😮", "😢", "🙏"]

// The emoji that the user set on a Message, or "".
function myReaction(message) {
  var mine = (message.reactions || []).filter(function (r) { return r.fromMe })
  return mine.length ? mine[0].emoji : ""
}

// The emoji to send when the user picks `emoji` on a Message: picking the current Reaction removes it.
function reactionChoice(message, emoji) {
  return myReaction(message) === emoji ? "" : emoji
}

function reactRequest(id, jid, messageId, emoji) {
  return request(id, "chat.react", { chat: jid, id: messageId, emoji: emoji })
}

// The Reactions of a Message as "👍 2" chips, in the order the emoji were first used. A Tombstone shows none.
// `mine` marks the emoji that the user set. `names` lists who reacted, for the hover text.
function reactionChips(message) {
  if (message.revoked) return []
  var order = []
  var chips = {}
  ;(message.reactions || []).forEach(function (r) {
    if (!chips[r.emoji]) { chips[r.emoji] = { emoji: r.emoji, count: 0, mine: false, names: [] }; order.push(r.emoji) }
    var chip = chips[r.emoji]
    chip.count++
    chip.mine = chip.mine || r.fromMe === true
    chip.names.push(r.fromMe ? "You" : r.senderName)
  })
  return order.map(function (emoji) { chips[emoji].names = chips[emoji].names.join(", "); return chips[emoji] })
}

// The Quote above a reply: who wrote the quoted Message and a one-line preview. Null when the Message has no Quote.
var QUOTE_PREVIEW_LENGTH = 80

function quoteView(message) {
  var quote = message.quote
  if (!quote) return null
  var text = String(quote.text).replace(/\s+/g, " ").trim()
  if (text.length > QUOTE_PREVIEW_LENGTH) text = text.slice(0, QUOTE_PREVIEW_LENGTH - 1) + "…"
  return { id: quote.id, sender: quote.fromMe ? "You" : quote.senderName, text: text }
}

// A Message can be quoted unless it is a Tombstone.
function canQuoteMessage(message) {
  return !message.revoked
}

// A Message can get a Reaction once WhatsApp has it: any Message of others, or a Message of the user that was sent.
function canReactToMessage(message) {
  return !message.revoked && (!message.fromMe || ["sent", "delivered", "read", "played"].indexOf(message.state) >= 0)
}

// The one rule for "which actions does this Message allow". The toolbar, the menu and the Media links read it.
// `m` is a Message as in docs/contract.md.
function actionsFor(m) {
  if (m.revoked) return ["deleteForMe"]
  var own = m.fromMe === true
  var kind = m.media ? m.media.kind : ""
  var state = m.media ? m.media.state : ""
  var a = ["reply", "react"]
  if (m.text) a.push("copy")
  if (own && m.text && !m.media && m.state !== "failed") a.push("edit")
  if (m.state === "failed") a.push("retry")
  if (m.media && kind !== "sticker") {
    if (state === "none" || state === "failed") a.push("download")
    if (state === "ready" && (kind === "voice" || kind === "audio")) a.push("play")
    if (state === "ready" && kind !== "voice") a.push("open")
    if (state === "ready") a.push("saveAs")
  }
  a.push(own ? "deleteForEveryone" : "deleteForMe")
  return a
}

// The actions for the menu: the rule, minus react and edit when the Message cannot take them now.
function menuActions(m, nowSeconds) {
  return actionsFor(m).filter(function (name) {
    if (name === "react") return canReactToMessage(m)
    if (name === "edit") return canEditMessage(m, nowSeconds)
    return true
  })
}

var ACTION_LABELS = {
  reply: "Reply", react: "React", copy: "Copy text", edit: "Edit", retry: "Retry send",
  download: "Download", play: "Play", open: "Open", saveAs: "Save as…",
  deleteForMe: "Delete for me", deleteForEveryone: "Delete for everyone"
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

// What a notification says for a Message with Media and no text. Keys are the Media kinds of docs/contract.md.
var MEDIA_LABELS = {
  image: "Photo",
  sticker: "Sticker",
  voice: "Voice message",
  audio: "Audio",
  video: "Video",
  document: "Document"
}

// The command that shows a `notify` frame as a desktop notification. A click runs the Plugin IPC handler `openChat`.
// A Group Chat shows the sender in front of the text. The headline cannot start with "-": the sender script would read an option.
// --exec comes last, and each word is one argument, so a Chat id never becomes a command.
function notifyCommand(params) {
  var headline = params.name.charAt(0) === "-" ? " " + params.name : params.name
  var text = params.text || MEDIA_LABELS[params.media] || ""
  var body = params.kind === "group" && params.senderName ? params.senderName + ": " + text : text
  return ["omarchy-notification-send", "--app-name", "whisker", headline, body,
    "--exec", "omarchy-shell", "pauwelz.whisker", "openChat", params.chat]
}

// True when the frame asks the Plugin to show a desktop notification.
function isNotify(frame) {
  return !!frame && frame.method === "notify"
}

// The Chat of the Chat list with this id. A Chat that is not loaded gets a minimal row, so a click on a notification still opens it.
// shortcut: the fallback kind is "direct", so a Group Chat that is not loaded shows no sender names until the list loads it.
function chatByJid(state, jid) {
  var found = (state.chats || []).filter(function(chat) { return chat.jid === jid })
  return found.length > 0 ? found[0] : { jid: jid, name: jid, kind: "direct" }
}

// Profile pictures. `pictures[jid]` is the base64 JPEG of a Chat, or "" when it has none. No entry means not asked yet.
function chatPictureRequest(id, jid) {
  return request(id, "chat.picture", { chat: jid })
}

function pictureLoaded(state, result) {
  var pictures = withChanges(state.pictures || {}, {})
  pictures[result.chat] = result.data
  return withChanges(state, { pictures: pictures })
}

function pictureChanged(state, params) {
  var pictures = withChanges(state.pictures || {}, {})
  delete pictures[params.chat]
  return withChanges(state, { pictures: pictures })
}

// The JID of the open Chat when it has no picture entry yet, else "". The Plugin asks for that picture.
function pictureToRequest(state) {
  var chat = state.openChat
  return chat && !(state.pictures && state.pictures[chat.jid] !== undefined) ? chat.jid : ""
}

// The picture of a Chat as a data URL, or "" when it has none.
function chatPictureSource(state, jid) {
  var data = state.pictures ? state.pictures[jid] : ""
  return data ? "data:image/jpeg;base64," + data : ""
}

// Presence. `presence[jid]` is { online, lastSeen } of a Participant of a Direct Chat.
// `activity[chat]` lists who is composing or recording there: { participant, name, state }.
function presenceChanged(state, params) {
  var presence = withChanges(state.presence || {}, {})
  presence[params.chat] = { online: params.online, lastSeen: params.lastSeen }
  return withChanges(state, { presence: presence })
}

function activityChanged(state, p) {
  var activity = withChanges(state.activity || {}, {})
  var others = (activity[p.chat] || []).filter(function(a) { return a.participant !== p.participant })
  if (p.state !== "paused") others.push({ participant: p.participant, name: p.name, state: p.state })
  activity[p.chat] = others
  return withChanges(state, { activity: activity })
}

// The header line of a Direct Chat: "Online", "last seen at 14:05" (within a day), "last seen 2026-10-23", or "".
function presenceLabel(state, nowSeconds) {
  // With "send online" off, WhatsApp may stop sending Presence, so Online and Last Seen would go stale.
  if (state.settings && state.settings.sendOnline === false) return ""
  var chat = state.openChat
  var p = chat && chat.kind !== "group" && state.presence ? state.presence[chat.jid] : null
  if (!p) return ""
  if (p.online) return "Online"
  if (!p.lastSeen) return ""
  if (nowSeconds - p.lastSeen < 24 * 3600) return "last seen at " + timeLabel(p.lastSeen)
  return "last seen " + dayLabel(p.lastSeen)
}

// "typing…" or "recording…" in a Direct Chat; "Bob is typing…" in a Group Chat. Empty when nobody is active.
function activityLabel(state) {
  return state.openChat ? chatActivityLabel(state, state.openChat) : ""
}

// The same label for any Chat (`chat` has jid and kind), for the Chat list.
function chatActivityLabel(state, chat) {
  var list = state.activity ? state.activity[chat.jid] || [] : []
  return list.map(function(a) {
    var verb = a.state === "recording" ? "recording…" : "typing…"
    return chat.kind === "group" ? a.name + " is " + verb : verb
  }).join(", ")
}

// The Chat list. The filters sit next to the search field. The Daemon sends the Chats Pinned first.
var CHAT_FILTERS = [
  { value: "all", label: "all" },
  { value: "unread", label: "unread" },
  { value: "groups", label: "groups" },
  { value: "archived", label: "archived" }
]

// shortcut: the filter works on the loaded pages only, upgrade to a Daemon param if "Load more" feels wrong with a filter.
// The Daemon sends only the archived Chats for "archived", so that filter keeps every row.
function filterChats(chats, filter) {
  return chats.filter(function(chat) {
    if (filter === "unread") return chat.unread === true
    return filter !== "groups" || chat.kind === "group"
  })
}

var CHAT_SIGNS = { direct: "◦", group: "#", self: "◈" }

function chatSign(chat) {
  return CHAT_SIGNS[chat.kind] || CHAT_SIGNS.direct
}

var MAX_COUNT = 99

// The unread mark: the count, a dot when the phone marked the Chat unread without a count, or "".
function chatCountLabel(chat) {
  if (!chat.unread) return ""
  if (!chat.unreadCount) return "●"
  return chat.unreadCount > MAX_COUNT ? MAX_COUNT + "+" : String(chat.unreadCount)
}

var MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

// The time of the last activity: the clock for today, the date before. "" when the Chat has none.
function chatTimeLabel(lastActive, nowSeconds) {
  if (!lastActive) return ""
  var at = new Date(lastActive * 1000)
  var now = new Date(nowSeconds * 1000)
  if (at.toDateString() === now.toDateString()) return timeLabel(lastActive)
  return at.getDate() + " " + MONTHS[at.getMonth()]
}

// The mute choices of the Chat links. `seconds` is -1 for ever.
var MUTE_CHOICES = [
  { label: "8h", seconds: 8 * 3600 },
  { label: "1w", seconds: 7 * 86400 },
  { label: "always", seconds: -1 }
]

// The mute end time of a choice, in Unix seconds as `chat.mute` takes it: -1 for ever.
function muteEnd(choice, nowSeconds) {
  return choice.seconds < 0 ? -1 : nowSeconds + choice.seconds
}

var UNMUTE = { until: 0 }

// How long a Muted Chat stays muted: "for ever", "2d left", "3h left" or "1m left".
function muteLeftLabel(chat, nowSeconds) {
  if (chat.mutedUntil === -1) return "for ever"
  var left = chat.mutedUntil - nowSeconds
  if (left >= 86400) return Math.floor(left / 86400) + "d left"
  if (left >= 3600) return Math.floor(left / 3600) + "h left"
  return Math.max(1, Math.floor(left / 60)) + "m left"
}

// The links of a Chat, on hover in the Chat list and in the Chat header: pin, mark unread and mute, or their opposites.
// A link has `text` and `option`; `option` has `pinned`, `unread` or `until`, or is null for mute: it shows the mute choices.
// With `muting`, the links are the mute choices, with end times from `nowSeconds`.
function chatLinks(chat, muting, nowSeconds) {
  if (muting) return MUTE_CHOICES.map(function(choice) {
    return { text: choice.label, option: { until: muteEnd(choice, nowSeconds) } }
  })
  return [
    { text: chat.pinned ? "unpin" : "pin", option: { pinned: !chat.pinned } },
    { text: chat.unread ? "mark read" : "mark unread", option: { unread: !chat.unread } },
    chat.muted ? { text: "unmute", option: UNMUTE } : { text: "mute", option: null }
  ]
}

// The request for the option of a Chat link.
function chatOptionRequest(id, jid, option) {
  if (option.pinned !== undefined) return request(id, "chat.pin", { chat: jid, pinned: option.pinned })
  if (option.unread !== undefined) return request(id, "chat.markUnread", { chat: jid, unread: option.unread })
  return request(id, "chat.mute", { chat: jid, until: option.until })
}

// Marking the open Chat unread must close it: an open Chat is attended, and attending marks it read again.
function leavesChatFor(state, jid, option) {
  return option.unread === true && !!state.openChat && state.openChat.jid === jid
}

// The listed Chat of a jid, or null. The links of the open Chat need its pin and mute state.
function chatRow(state, jid) {
  var chats = state.chats || []
  for (var i = 0; i < chats.length; i++) if (chats[i].jid === jid) return chats[i]
  return null
}

// The text under the faded logo when no Chat shows.
function emptyChatsText(filter, query) {
  if (query) return "No chat matches “" + query + "”."
  if (filter === "unread") return "No unread chats."
  if (filter === "archived") return "No archived chats."
  return filter === "groups" ? "No group chats." : "No chats yet."
}

// What the composer text means for the outgoing Composing: "start", "stop" or "" (no change).
function typingAction(wasTyping, text) {
  var typing = String(text).trim() !== ""
  if (typing === wasTyping) return ""
  return typing ? "start" : "stop"
}

function typingRequest(id, jid, typing) {
  return request(id, "chat.typing", { chat: jid, typing: typing })
}

// The line under the Chat header: the activity ("typing…") wins over Online and Last Seen.
function chatStatusLabel(state, nowSeconds) {
  return activityLabel(state) || presenceLabel(state, nowSeconds)
}

// Media. The Daemon sends paths only on request (media.open). `mediaPaths` holds them by Chat and Message id.
// An empty path means the request is out. Views show the path only when it is not empty.
function mediaKey(chat, id) {
  return chat + "|" + id
}

function mediaPathEntry(result) {
  var entry = {}
  entry[mediaKey(result.chat, result.id)] = result.path
  return entry
}

function mediaPath(state, message) {
  return (state.mediaPaths || {})[mediaKey(message.chat, message.id)] || ""
}

// A sticker shows inside the pop-out once the Daemon holds it. An image shows only as the thumbnail of its chip.
function showsInline(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY && message.media.kind === "sticker"
}

// A ready image without WhatsApp's thumbnail: its chip shows the downloaded file as the thumbnail.
function previewsFile(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY && message.media.kind === "image" && !message.media.thumbnail
}

// The Messages of the open Chat whose path the Plugin must ask for. markMediaRequested then stops a second request.
function mediaToLoad(state) {
  if (!state.openChat) return []
  var known = state.mediaPaths || {}
  return state.openChat.messages.filter(function(m) { return (showsInline(m) || previewsFile(m)) && known[mediaKey(m.chat, m.id)] === undefined })
}

function markMediaRequested(state, messages) {
  var entries = {}
  messages.forEach(function(m) { entries[mediaKey(m.chat, m.id)] = "" })
  return withChanges(state, { mediaPaths: withChanges(state.mediaPaths || {}, entries) })
}

var MEDIA_KIND_LABELS = { image: "Image", sticker: "Sticker", voice: "Voice note", video: "Video", audio: "Audio", document: "Document" }

// The one line that stands for Media in the timeline, for example "Document: report.pdf (1.2 MB)".
function mediaLabel(message) {
  var media = message.media
  if (!media) return ""
  var label = media.gif ? "GIF" : MEDIA_KIND_LABELS[media.kind] || "File"
  if (media.name) label += ": " + media.name
  return label + " (" + (media.duration ? formatDuration(media.duration) + ", " : "") + formatSize(media.size) + ")"
}

// Seconds as "m:ss", for the length of audio, voice and video.
function formatDuration(seconds) {
  var s = Math.round(seconds)
  return Math.floor(s / 60) + ":" + (s % 60 < 10 ? "0" : "") + (s % 60)
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + " B"
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB"
  return (bytes / (1024 * 1024)).toFixed(1) + " MB"
}

// The state text of Media that is not ready: "" when there is nothing to say.
function mediaStatus(message) {
  if (!message.media) return ""
  if (message.media.state === MEDIA_STATE.DOWNLOADING) return "Downloading…"
  return message.media.state === MEDIA_STATE.FAILED ? "Download failed" : ""
}

function isDownloading(message) {
  return !!message.media && message.media.state === MEDIA_STATE.DOWNLOADING
}

function canDownloadMedia(message) {
  return !!message.media && (message.media.state === MEDIA_STATE.NONE || message.media.state === MEDIA_STATE.FAILED)
}

function canOpenMedia(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY
}

// The command that opens a plain copy: video in mpv, a GIF in mpv on a loop, a voice note in mpv without a window,
// everything else in the default app.
function openCommand(message, path) {
  if (message.media.kind === "voice") return ["mpv", "--no-video", path]
  if (message.media.gif) return ["mpv", "--loop", path]
  return message.media.kind === "video" ? ["mpv", path] : ["xdg-open", path]
}

// Voice notes play in the pop-out. `playingKey` is the mediaKey of the note that plays now, or "".
function isVoice(message) {
  return !!message.media && message.media.kind === "voice"
}

function isPlaying(playingKey, message) {
  return playingKey !== "" && playingKey === mediaKey(message.chat, message.id)
}

// The label of the button that opens Media: Play or Stop for a voice note, Open for the rest.
function openLabel(message, playingKey) {
  if (!isVoice(message)) return "Open"
  return isPlaying(playingKey, message) ? "Stop" : "Play"
}

// The Daemon sends Played (and Read for that Message in On interaction mode) when playback starts.
function playedRequest(id, message) {
  return request(id, "media.played", { chat: message.chat, id: message.id })
}


function mediaDownloadRequest(id, message) {
  return request(id, "media.download", { chat: message.chat, id: message.id })
}

function mediaOpenRequest(id, message) {
  return request(id, "media.open", { chat: message.chat, id: message.id })
}

// `path` is the file the user chose in the Save as dialog.
function mediaSaveRequest(id, message, path) {
  return request(id, "media.save", { chat: message.chat, id: message.id, path: path })
}

// The Plugin sends this when a Chat closes, so the Daemon deletes the plain copies.
function mediaReleaseRequest(id, jid) {
  return request(id, "media.release", { chat: jid })
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

// True when the download of the Media of a Message failed.
function mediaFailed(message) {
  return !!message.media && message.media.state === MEDIA_STATE.FAILED
}

function downloadLabel(message) {
  return mediaFailed(message) ? "Retry download" : "Download"
}

// The Quote above the composer: the Message that the next send quotes, shown like the Quote of a sent reply. Null for none.
function composerQuoteView(quoting) {
  if (!quoting) return null
  return quoteView({ quote: { fromMe: quoting.fromMe, senderName: quoting.senderName, text: messageBody(quoting) } })
}

// What to do when the user asks to open Media: "stop" the playing note, "launch" at once (path known), or "request" the path.
function openMediaStep(state, playingKey, message) {
  if (isPlaying(playingKey, message)) return { action: "stop", path: "" }
  var path = mediaPath(state, message)
  return path !== "" ? { action: "launch", path: path } : { action: "request", path: "" }
}

// Media the user clicked to open, by mediaKey, until its path arrives. These two keep that table without changing it.
function rememberOpen(pending, message) {
  var entry = {}
  entry[mediaKey(message.chat, message.id)] = message
  return withChanges(pending, entry)
}

// A media.open result: the Message that waits for this path (or null), and the table without it.
function takeOpen(pending, result) {
  var key = mediaKey(result.chat, result.id)
  if (!pending[key]) return { message: null, pending: pending }
  var rest = {}
  for (var k in pending) if (k !== key) rest[k] = pending[k]
  return { message: pending[key], pending: rest }
}

// What a Daemon frame means for Media. Returns { state, pending, launch, load }:
// launch is { message, path } for Media the user clicked and whose path just arrived, or null;
// load lists the Messages whose path the Plugin must ask for now (state already marks them as asked).
function mediaFrameStep(state, pending, frame) {
  var result = frame.result
  var launch = null
  if (result && result.path) {
    var taken = takeOpen(pending, result)
    pending = taken.pending
    if (taken.message) launch = { message: taken.message, path: result.path }
  }
  var load = mediaToLoad(state)
  if (load.length > 0) state = markMediaRequested(state, load)
  return { state: state, pending: pending, launch: launch, load: load }
}

// Media chips. A ready sticker shows as itself, so it gets no chip.
function chipShown(m) {
  return !!m.media && !(m.media.kind === "sticker" && m.media.state === MEDIA_STATE.READY)
}

// WhatsApp's thumbnail as a data URL. Without one, the downloaded file of an image (`path`, from mediaPath), else "".
function thumbnailSource(m, path) {
  if (!m.media) return ""
  if (m.media.thumbnail) return "data:image/jpeg;base64," + m.media.thumbnail
  return previewsFile(m) && path ? "file://" + path : ""
}

// A video that is not downloaded shows its thumbnail blurred.
function chipBlurred(m) {
  return !!m.media && m.media.kind === "video" && m.media.state !== MEDIA_STATE.READY
}

var MIME_BADGES = { "application/pdf": "PDF", "audio/mpeg": "MP3", "audio/ogg": "OGG", "audio/mp4": "M4A", "audio/wav": "WAV", "audio/flac": "FLAC" }

// The file-type badge of a chip without a thumbnail. An image, video or GIF says IMG, VID or GIF.
// A document or audio file shows the extension of the name, else the MIME type, else the kind.
function fileBadge(m) {
  var md = m.media
  if (!md || md.thumbnail) return ""
  if (md.kind === "image") return "IMG"
  if (md.kind === "video") return md.gif ? "GIF" : "VID"
  if (md.kind !== "document" && md.kind !== "audio") return ""
  var ext = /\.([A-Za-z0-9]{1,5})$/.exec(md.name || "")
  if (ext) return ext[1].toUpperCase()
  return MIME_BADGES[String(md.mime).split(";")[0]] || (md.kind === "audio" ? "AUDIO" : "FILE")
}

// The action of a click on the thumbnail or the name: download, play or open. "" while downloading and for a sticker.
function chipMainAction(m) {
  var a = actionsFor(m)
  var main = ["download", "play", "open"].filter(function(x) { return a.indexOf(x) >= 0 })
  return main.length > 0 ? main[0] : ""
}

var CHIP_LINKS = ["download", "play", "open", "saveAs"]

// The links under the chip, from the actions rule. A voice note has a play button instead of a Play link.
function chipLinks(m) {
  var failed = !!m.media && m.media.state === MEDIA_STATE.FAILED
  return actionsFor(m).filter(function(x) { return CHIP_LINKS.indexOf(x) >= 0 && !(x === "play" && m.media.kind === "voice") }).map(function(x) {
    return { action: x, label: x === "download" && failed ? "retry" : ACTION_LABELS[x].toLowerCase() }
  })
}

// A received voice note that is ready and not played yet. The Daemon sets `media.played` on the first play.
function unplayedDot(m) {
  return !!m.media && m.media.kind === "voice" && m.media.state === MEDIA_STATE.READY && m.fromMe !== true && m.media.played !== true
}

// One wheel notch scrolls a list this far. Qt's default wheel flick is slow in a long timeline.
var WHEEL_STEP = 60

// The contentY a list scrolls to on a wheel event: the touchpad's pixels when it sends them, else a fixed step per notch
// (angleY is 120 per notch). It stays between top and bottom; a bottom above the top (short content) keeps it at the top.
function wheelScrollTarget(contentY, angleY, pixelY, top, bottom) {
  var delta = pixelY !== 0 ? pixelY : angleY / 120 * WHEEL_STEP
  return Math.max(top, Math.min(Math.max(top, bottom), contentY - delta))
}
