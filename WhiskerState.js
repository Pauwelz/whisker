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
      return linking(state, { qr: params.qr, reason: "", message: "Scan this QR code in WhatsApp: Linked devices, Link a device.", canRetry: false })
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
  messageChanged: { fold: function (state, params) { return upsertMessage(state, params.message) } },
  chatMerged: { fold: chatMerged },
  presenceChanged: { fold: presenceChanged },
  activityChanged: { fold: activityChanged }
}

// The row of a notification, or null for a response and for a method with no row.
function effectsOf(frame) {
  if (!frame || !Object.prototype.hasOwnProperty.call(NOTIFICATIONS, frame.method)) return null
  return NOTIFICATIONS[frame.method]
}

function foldFrame(state, frame) {
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
function chatListRequest(id, offset, query) {
  var params = { offset: offset }
  if (query) params.query = query
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
  { value: "off", label: "Off" },
  { value: "onOpen", label: "On open" },
  { value: "onInteraction", label: "On interaction" }
]

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

// The Delivery State labels of the Messages of the user. Mirror of docs/contract.md.
var DELIVERY_LABELS = {
  pending: "Sending…",
  sent: "Sent",
  delivered: "Delivered",
  read: "Read",
  played: "Played",
  failed: "Failed"
}

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
  if (frame.result && frame.result.path) return withChanges(state, { mediaPaths: withChanges(state.mediaPaths || {}, mediaPathEntry(frame.result)) })
  if (frame.result && frame.result.messages) return messagesPage(state, frame.result)
  if (frame.result && frame.result.message) return upsertMessage(state, frame.result.message)
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
  return withChanges(next, { openChat: withChanges(next.openChat, { moreMessages: result.more === true }) })
}

// The Draft of the open Chat: { text, quote, edit, attachment }. The Daemon stores it.
// `quote` and `edit` are Message ids or "". `attachment` is the absolute path of the staged file or "".
var EMPTY_DRAFT = { text: "", quote: "", edit: "", attachment: "" }

function draftOf(state) {
  return (state.openChat && state.openChat.draft) || EMPTY_DRAFT
}

// A draft.get result. The first one for the open Chat sets the Draft; a Draft that the user already changed stays.
function draftLoaded(state, result) {
  var chat = state.openChat
  if (!chat || chat.jid !== result.chat || chat.draft !== undefined) return state
  return withChanges(state, { openChat: withChanges(chat, { draft: result.draft }) })
}

// Changes the Draft of the open Chat. Returns the same state when nothing changes.
// A staged file never combines with an Edit or a Quote. Starting an Edit or a Quote drops the file.
// Staging a file drops the Quote, and is refused during an Edit.
function changeDraft(state, changes) {
  if (!state.openChat) return state
  var old = draftOf(state)
  var draft = withChanges(old, changes)
  if (draft.attachment && (draft.edit || draft.quote)) {
    if (!changes.attachment) draft = withChanges(draft, { attachment: "" })
    else if (draft.edit) return state
    else draft = withChanges(draft, { quote: "" })
  }
  if (draft.text === old.text && draft.quote === old.quote && draft.edit === old.edit && draft.attachment === old.attachment) return state
  return withChanges(state, { openChat: withChanges(state.openChat, { draft: draft }) })
}

function clearDraft(state) {
  return changeDraft(state, EMPTY_DRAFT)
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
  return request(id, "draft.set", { chat: jid, text: draft.text, quote: draft.quote, edit: draft.edit, attachment: draft.attachment })
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

// The path of a file:// URL that FileDialog gives. The Daemon needs a path.
function filePath(url) {
  return decodeURIComponent(String(url).replace(/^file:\/\//, ""))
}

// Whether the composer shows the Attach button: not during an Edit, which never combines with a file.
function canAttach(editingId) {
  return editingId === ""
}

// Whether the composer shows the staged file row (name and remove button).
function hasAttachment(draft) {
  return draft.attachment !== ""
}

// The file name of the staged file, shown in the composer.
function attachmentName(draft) {
  return draft.attachment.split("/").pop()
}

// The request to send the file at `path` as Media.
function sendMediaRequest(id, jid, path, caption) {
  return request(id, "chat.sendMedia", { chat: jid, path: path, caption: caption })
}

function retryMessageRequest(id, jid, messageId) {
  return request(id, "chat.retry", { chat: jid, id: messageId })
}

// What the composer does with a key press: "send", "swallow" (Enter with nothing to send) or "default" (the text field handles it).
// Enter sends and Shift+Enter adds a line. These keybindings are fixed.
// A staged file can go without text.
function composerAction(key, shift, text, hasAttachment) {
  if (key !== KEY_RETURN && key !== KEY_ENTER) return "default"
  if (shift) return "default"
  return String(text).trim() === "" && !hasAttachment ? "swallow" : "send"
}

// The fixed shortcuts of the pop-out. `shortcut` is "Escape" or "Ctrl+F"; `local` is { showSettings } as in panelParts.
// Escape goes back one level: Chat, then settings, then it closes the pop-out. Ctrl+F searches, on the Chat list only.
// Returns "closeChat", "closeSettings", "closePopout", "focusSearch" or "none".
function shortcutAction(shortcut, state, local) {
  if (shortcut === "Escape") {
    if (state.openChat) return "closeChat"
    return local.showSettings ? "closeSettings" : "closePopout"
  }
  var onList = panelParts(state, { pairMode: false, showSettings: local.showSettings }).chatList
  return shortcut === "Ctrl+F" && onList ? "focusSearch" : "none"
}

// The sender name shown on a Message: only on the Messages of others in a Group Chat.
function senderLabel(chat, message) {
  return chat.kind === "group" && !message.fromMe ? message.senderName : ""
}

// The Delivery State label of a Message. Messages of others have none.
function deliveryLabel(message) {
  return message.fromMe ? DELIVERY_LABELS[message.state] || "" : ""
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
  return message.edited && !message.revoked ? "Edited" : ""
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

// The request for the composer text: an edit of `editingId`, or a new Message when `editingId` is empty.
// A new Message quotes `quoteId` when it is set.
// A staged file (`attachment`, a path) goes as Media with the text as caption. The Draft never holds a Quote with a file.
function composerRequest(id, jid, editingId, text, quoteId, attachment) {
  if (editingId) return editRequest(id, jid, editingId, text)
  return attachment ? sendMediaRequest(id, jid, attachment, text) : sendRequest(id, jid, text, quoteId)
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
function reactionChips(message) {
  if (message.revoked) return []
  var order = []
  var counts = {}
  ;(message.reactions || []).forEach(function (r) {
    if (!counts[r.emoji]) { counts[r.emoji] = 0; order.push(r.emoji) }
    counts[r.emoji]++
  })
  return order.map(function (emoji) { return { emoji: emoji, count: counts[emoji] } })
}

// The Quote above a reply: who wrote the quoted Message and a one-line preview. Null when the Message has no Quote.
var QUOTE_PREVIEW_LENGTH = 80

function quoteView(message) {
  var quote = message.quote
  if (!quote) return null
  var text = String(quote.text).replace(/\s+/g, " ").trim()
  if (text.length > QUOTE_PREVIEW_LENGTH) text = text.slice(0, QUOTE_PREVIEW_LENGTH - 1) + "…"
  return { sender: quote.fromMe ? "You" : quote.senderName, text: text }
}

// A Message can be quoted unless it is a Tombstone.
function canQuoteMessage(message) {
  return !message.revoked
}

// A Message can get a Reaction once WhatsApp has it: any Message of others, or a Message of the user that was sent.
function canReactToMessage(message) {
  return !message.revoked && (!message.fromMe || ["sent", "delivered", "read", "played"].indexOf(message.state) >= 0)
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

function pad2(n) {
  return (n < 10 ? "0" : "") + n
}

// The header line of a Direct Chat: "Online", "last seen at 14:05" (within a day), "last seen 2026-10-23", or "".
function presenceLabel(state, nowSeconds) {
  var chat = state.openChat
  var p = chat && chat.kind !== "group" && state.presence ? state.presence[chat.jid] : null
  if (!p) return ""
  if (p.online) return "Online"
  if (!p.lastSeen) return ""
  var d = new Date(p.lastSeen * 1000)
  if (nowSeconds - p.lastSeen < 24 * 3600) return "last seen at " + pad2(d.getHours()) + ":" + pad2(d.getMinutes())
  return "last seen " + d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate())
}

// "typing…" or "recording…" in a Direct Chat; "Bob is typing…" in a Group Chat. Empty when nobody is active.
function activityLabel(state) {
  var chat = state.openChat
  var list = chat && state.activity ? state.activity[chat.jid] || [] : []
  return list.map(function(a) {
    var verb = a.state === "recording" ? "recording…" : "typing…"
    return chat.kind === "group" ? a.name + " is " + verb : verb
  }).join(", ")
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

// Images and stickers show inside the pop-out once the Daemon holds them.
function showsInline(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY && (message.media.kind === "image" || message.media.kind === "sticker")
}

// The Messages of the open Chat whose path the Plugin must ask for. markMediaRequested then stops a second request.
function mediaToLoad(state) {
  if (!state.openChat) return []
  var known = state.mediaPaths || {}
  return state.openChat.messages.filter(function(m) { return showsInline(m) && known[mediaKey(m.chat, m.id)] === undefined })
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
  var label = MEDIA_KIND_LABELS[media.kind] || "File"
  if (media.name) label += ": " + media.name
  return label + " (" + formatSize(media.size) + ")"
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

function canDownloadMedia(message) {
  return !!message.media && (message.media.state === MEDIA_STATE.NONE || message.media.state === MEDIA_STATE.FAILED)
}

function canOpenMedia(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY
}

// The command that opens a plain copy: video in mpv, a voice note in mpv without a window, everything else in the default app.
function openCommand(message, path) {
  if (message.media.kind === "voice") return ["mpv", "--no-video", path]
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
    pairToggle: linking,
    chatList: linked && !local.showSettings && !chat,
    loadMoreChats: linked && state.moreChats === true && !local.showSettings && !chat,
    settingsButton: linked && !!state.settings && !chat,
    settings: linked && local.showSettings && !!state.settings,
    chat: linked && !local.showSettings && chat
  }
}

// The text of a settings button: "Send online: On".
function onOffLabel(title, on) {
  return title + ": " + (on ? "On" : "Off")
}

// The text of a Read Receipts mode button. The mode that is set is marked.
function readReceiptLabel(mode, settings) {
  return "Read Receipts: " + mode.label + (settings && settings.readReceipts === mode.value ? " (selected)" : "")
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
