.pragma library

.import "Common.js" as Common

// The new Chat view: the contact search, the phone number lookup and the rows.

// The request for the people the new Chat view lists. The Daemon returns the first 50 that match the query.
function contactsSearchRequest(id, query) {
  return Common.request(id, "contacts.search", { query: query })
}

// The request to store the Chat with a person, before the first Message or Draft.
function chatStartRequest(id, jid) {
  return Common.request(id, "chat.start", { jid: jid })
}

// The request to ask WhatsApp if a phone number is on WhatsApp.
function lookupRequest(id, phone) {
  return Common.request(id, "contacts.lookup", { phone: phone })
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
  return Common.withChanges(state, { lookup: { phone: phone, status: "checking", jid: "" } })
}

// The reply names its number. A reply for another number than the one in the row is late and changes nothing.
function lookupAnswered(state, result) {
  if (!state.lookup || state.lookup.phone !== result.phone) return state
  return Common.withChanges(state, { lookup: { phone: result.phone, status: result.jid ? "found" : "none", jid: result.jid } })
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
  offline: Common.NO_WHATSAPP
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

// The name a new Chat row shows: the number for a "phone" row, the name for a contact row.
function newChatRowName(row) {
  return row.kind === "phone" ? row.phone : row.name
}

// What startChat gets for a tapped row. A "phone" row has no name, so the number is its name.
function newChatTarget(row) {
  return row.kind === "phone" ? { jid: row.jid, name: newChatRowName(row) } : row
}
