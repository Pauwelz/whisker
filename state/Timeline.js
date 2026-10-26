.pragma library

.import "Common.js" as Common

// The timeline of the open Chat: opening, paging, On-Demand History, rows, senders, time labels, delivery marks and wheel scrolling.

// Opens a Chat of the Chat list. `chat` is a Chat of the list: jid, name, kind. Messages arrive with chat.messages.
function openChat(state, chat) {
  var open = { jid: chat.jid, name: chat.name, kind: chat.kind, messages: [], moreMessages: false }
  if (chat.pushNameOnly) open.pushNameOnly = true
  return Common.withChanges(state, { openChat: open })
}

function closeChat(state) {
  var next = Common.withChanges(state, {})
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
      return Common.withChanges(state, { openChat: Common.withChanges(chat, { messages: messages }) })
    }
  }
  var at = messages.length
  while (at > 0 && messages[at - 1].time > message.time) at--
  messages.splice(at, 0, message)
  return Common.withChanges(state, { openChat: Common.withChanges(chat, { messages: messages }) })
}

// Two Chats became one. The open Chat takes the JID of the merged Chat and shows no Messages until they load again.
function chatMerged(state, merged) {
  var chat = state.openChat
  if (!chat || (chat.jid !== merged.from && chat.jid !== merged.to)) return state
  return Common.withChanges(state, { openChat: Common.withChanges(chat, { jid: merged.to, messages: [], moreMessages: false }) })
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
  return Common.withChanges(state, { openChat: Common.withChanges(state.openChat, { historyLoading: true }) })
}

function historyRequest(id, jid) {
  return Common.request(id, "chat.requestHistory", { chat: jid })
}

function historyReceived(state, params) {
  var chat = state.openChat
  if (!chat || chat.jid !== params.chat) return state
  var next = Common.withChanges(state, { openChat: Common.withChanges(chat, { historyLoading: undefined }) })
  if (params.timedOut) return Common.withChanges(next, { notice: NO_ANSWER })
  if (params.count > 0) return next
  var done = Common.withChanges(state.nothingMore || {}, {})
  done[chat.jid] = true
  return Common.withChanges(next, { nothingMore: done })
}

// True when the Plugin must load the Messages that On-Demand History Sync stored. Call it with the state after the frame.
function reloadsHistory(state, frame) {
  return !!frame && frame.method === "historyReceived" && !frame.params.timedOut && frame.params.count > 0 &&
    !!state.openChat && state.openChat.jid === frame.params.chat
}

// A request that finds no connection gets no historyReceived, so the wait ends here.
function stopWaitingForPhone(state, code) {
  if (String(code) !== "-32007" || !state.openChat) return state
  return Common.withChanges(state, { openChat: Common.withChanges(state.openChat, { historyLoading: undefined }) })
}

// Takes an expired Message out of the open Chat.
function removeMessage(state, expired) {
  var chat = state.openChat
  if (!chat || chat.jid !== expired.chat) return state
  var messages = chat.messages.filter(function (m) { return m.id !== expired.id })
  return Common.withChanges(state, { openChat: Common.withChanges(chat, { messages: messages }) })
}

// Takes the Messages of a cleared Chat out of the open Chat: those at or before `until` (Unix seconds), or all when it is 0.
function clearMessages(state, cleared) {
  var chat = state.openChat
  if (!chat || chat.jid !== cleared.chat) return state
  var messages = chat.messages.filter(function (m) { return cleared.until > 0 && m.time > cleared.until })
  return Common.withChanges(state, { openChat: Common.withChanges(chat, { messages: messages }) })
}

// A chat.messages result for the open Chat. A page of another Chat is dropped.
function messagesPage(state, result) {
  var chat = state.openChat
  if (!chat || chat.jid !== result.chat) return state
  var next = state
  // shortcut: a Message with the same second as one already shown can land after it, upgrade to a (time, order) key if that shows.
  for (var i = 0; i < result.messages.length; i++) next = upsertMessage(next, result.messages[i])
  return Common.withChanges(next, { loadedChat: result.chat, openChat: Common.withChanges(next.openChat, { moreMessages: result.more === true }) })
}

// The request for one page of the Chat timeline. The next page starts at the number of Messages shown.
function messagesRequest(id, jid, offset) {
  return Common.request(id, "chat.messages", { chat: jid, offset: offset })
}

function retryMessageRequest(id, jid, messageId) {
  return Common.request(id, "chat.retry", { chat: jid, id: messageId })
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

// One wheel notch scrolls a list this far, the same rate as the Bitwarden plugin. Qt's default wheel flick is slow.
var WHEEL_STEP = 90

// The contentY a list scrolls to on a wheel event: a fixed step per notch (angleY is 120 per notch).
// A touchpad also sends angleY, in fractions of a notch. Its pixelDelta is much smaller and scrolls too slowly.
// It stays between top and bottom; a bottom above the top (short content) keeps it at the top.
function wheelScrollTarget(contentY, angleY, top, bottom) {
  var delta = angleY / 120 * WHEEL_STEP
  return Math.max(top, Math.min(Math.max(top, bottom), contentY - delta))
}
