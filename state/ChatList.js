.pragma library

.import "Common.js" as Common
.import "Timeline.js" as Timeline

// The Chat list: paging, filters, signs, counts, time labels, mute, chat options, pictures, presence and activity.

// A chats.list result. Page 0 replaces the Chat list, a later page extends it.
function chatListPage(state, result) {
  var next = {}
  for (var key in state) next[key] = state[key]
  next.chats = result.offset > 0 ? (state.chats || []).concat(result.chats) : result.chats
  next.moreChats = result.more === true
  next.unreadChats = result.unreadChats || 0
  return next
}

// The request for one page of the Chat list. The next page starts at the number of Chats shown.
// A non-empty query asks for the Chats that match the search field.
// archived asks for the archived Chats only. Without it, the list hides them.
function chatListRequest(id, offset, query, archived) {
  var params = { offset: offset }
  if (query) params.query = query
  if (archived) params.archived = true
  return Common.request(id, "chats.list", params)
}

// Whether a change of Chat filter changes the list the Daemon sends: only to or from "archived".
function archivedFilterChanged(current, next) {
  return (next === "archived") !== (current === "archived")
}

// The Chat of the Chat list with this id. A Chat that is not loaded gets a minimal row, so a click on a notification still opens it.
// shortcut: the fallback kind is "direct", so a Group Chat that is not loaded shows no sender names until the list loads it.
function chatByJid(state, jid) {
  var found = (state.chats || []).filter(function(chat) { return chat.jid === jid })
  return found.length > 0 ? found[0] : { jid: jid, name: jid, kind: "direct" }
}

// Profile pictures. `pictures[jid]` is the base64 JPEG of a Chat, or "" when it has none. No entry means not asked yet.
function chatPictureRequest(id, jid) {
  return Common.request(id, "chat.picture", { chat: jid })
}

function pictureLoaded(state, result) {
  var pictures = Common.withChanges(state.pictures || {}, {})
  pictures[result.chat] = result.data
  return Common.withChanges(state, { pictures: pictures })
}

function pictureChanged(state, params) {
  var pictures = Common.withChanges(state.pictures || {}, {})
  delete pictures[params.chat]
  return Common.withChanges(state, { pictures: pictures })
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
  var presence = Common.withChanges(state.presence || {}, {})
  presence[params.chat] = { online: params.online, lastSeen: params.lastSeen }
  return Common.withChanges(state, { presence: presence })
}

function activityChanged(state, p) {
  var activity = Common.withChanges(state.activity || {}, {})
  var others = (activity[p.chat] || []).filter(function(a) { return a.participant !== p.participant })
  if (p.state !== "paused") others.push({ participant: p.participant, name: p.name, state: p.state })
  activity[p.chat] = others
  return Common.withChanges(state, { activity: activity })
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
  if (nowSeconds - p.lastSeen < 24 * 3600) return "last seen at " + Timeline.timeLabel(p.lastSeen)
  return "last seen " + Timeline.dayLabel(p.lastSeen)
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

var CHAT_SIGNS = { direct: "󰀄", group: "󰡉", self: "󰃀" } // Nerd Font: account, account-group, bookmark

var PIN_MARK = "󰐃" // Nerd Font: pin

function chatSign(chat) {
  return CHAT_SIGNS[chat.kind] || CHAT_SIGNS.direct
}

// The name of a Chat on a line or in the header. A Push Name only gets "~" in front, and chatNumber shows the number after it.
// A line elides the name and never the number, so they are two texts.
function chatName(chat) {
  return chat.pushNameOnly ? "~ " + chat.name : chat.name
}

function chatNumber(chat) {
  return chat.pushNameOnly ? Timeline.phoneOf(chat.jid) : ""
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
  if (at.toDateString() === now.toDateString()) return Timeline.timeLabel(lastActive)
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
  if (option.pinned !== undefined) return Common.request(id, "chat.pin", { chat: jid, pinned: option.pinned })
  if (option.unread !== undefined) return Common.request(id, "chat.markUnread", { chat: jid, unread: option.unread })
  return Common.request(id, "chat.mute", { chat: jid, until: option.until })
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

// The line under the Chat header: the activity ("typing…") wins over Online and Last Seen.
function chatStatusLabel(state, nowSeconds) {
  return activityLabel(state) || presenceLabel(state, nowSeconds)
}
