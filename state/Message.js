.pragma library

.import "Common.js" as Common
.import "Timeline.js" as Timeline

// One Message: body, links, Edit, Revoke, Delete, Reactions, Quotes and the actions of its menu.

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
  return canRevokeMessage(message) && !(message.mentions && message.mentions.length) && nowSeconds - message.time <= EDIT_WINDOW_SECONDS
}

function editRequest(id, jid, messageId, text) {
  return Common.request(id, "chat.edit", { chat: jid, id: messageId, text: text })
}

function revokeRequest(id, jid, messageId) {
  return Common.request(id, "chat.revoke", { chat: jid, id: messageId })
}

function deleteForMeRequest(id, jid, messageId) {
  return Common.request(id, "chat.deleteForMe", { chat: jid, id: messageId })
}

// The Reaction picker: the six default emoji of WhatsApp. Mirror of `ReactionEmoji` in daemon/internal/whatsapp/whatsapp.go.
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
  return Common.request(id, "chat.react", { chat: jid, id: messageId, emoji: emoji })
}

// The Reactions of a Message as "👍 2" chips, in the order the emoji were first used. A Tombstone shows none.
// `mine` marks the emoji that the user set. `names` lists who reacted, for the hover text.
function reactionChips(message) {
  if (message.revoked) return []
  var order = []
  var chips = Object.create(null) // no prototype: an emoji key such as __proto__ stays a plain key
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
// An unverified Quote (`verified` false) is a snapshot that the sender wrote. It is never "You" and carries `unverified`.
var QUOTE_PREVIEW_LENGTH = 80

function quoteView(message) {
  var quote = message.quote
  if (!quote) return null
  var unverified = quote.verified === false
  var text = String(quote.revoked ? TOMBSTONE_TEXT : quote.text).replace(/\s+/g, " ").trim()
  if (text.length > QUOTE_PREVIEW_LENGTH) text = text.slice(0, QUOTE_PREVIEW_LENGTH - 1) + "…"
  var view = { id: quote.id, sender: quote.fromMe && !unverified ? "You" : Timeline.senderLabel(Object.assign({}, quote, { fromMe: false })), text: text }
  if (unverified) {
    view.unverified = true
    if (!view.sender) view.sender = "Unknown"
  }
  return view
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

// The Quote above the composer: the Message that the next send quotes, shown like the Quote of a sent reply. Null for none.
function composerQuoteView(quoting) {
  if (!quoting) return null
  return quoteView({ quote: { fromMe: quoting.fromMe, senderName: quoting.senderName, text: messageBody(quoting) } })
}
