.pragma library

.import "Common.js" as Common
.import "Message.js" as Message

// The Draft of the open Chat: text, Quote, Edit and Attachments, the send, paste and typing rules, and the composer keys.

// Qt key code of V: Ctrl+V pastes in the composer.
var KEY_V = 0x56

// The Draft of the open Chat: { text, quote, edit, attachments }. The Daemon stores it.
// `quote` and `edit` are Message ids or "". `attachments` are the absolute paths of the Attachments, in order.
// Attachments never combine with a Quote or an Edit.
// `openChat.unsendable` lists the Attachment paths that the Daemon refused. The marks live in the view only.
var EMPTY_DRAFT = { text: "", quote: "", edit: "", attachments: [] }

// The Daemon refuses text over 65536 code points (WhatsApp's limit). The Composer caps its text here.
var MAX_TEXT = 65536
var TEXT_TOO_LONG = "That text is too long"

function codePoints(text) {
  var n = 0
  for (var i = 0; i < text.length; i++) {
    var c = text.charCodeAt(i)
    if (!(c >= 0xDC00 && c <= 0xDFFF && i > 0 && text.charCodeAt(i - 1) >= 0xD800 && text.charCodeAt(i - 1) <= 0xDBFF)) n++
  }
  return n
}

// The cap on a text field: { text, ok }. A text over the limit gives `last`, the last text that was valid, and ok false.
function capText(text, last) {
  return codePoints(text) <= MAX_TEXT ? { text: text, ok: true } : { text: last, ok: false }
}

// The state with the notice for text that was too long.
function textRefused(state) {
  return Common.withChanges(state, { notice: TEXT_TOO_LONG })
}

function draftOf(state) {
  return (state.openChat && state.openChat.draft) || EMPTY_DRAFT
}

// A draft.get result. The first one for the open Chat sets the Draft and the marks; a Draft that the user already changed stays.
function draftLoaded(state, result) {
  var chat = state.openChat
  if (!chat || chat.jid !== result.chat || chat.draft !== undefined) return state
  var draft = Common.withChanges(result.draft, { attachments: result.draft.attachments || [] })
  return Common.withChanges(state, { openChat: Common.withChanges(chat, { draft: draft, unsendable: result.unsendable || [] }) })
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
  var draft = Common.withChanges(old, changes)
  if (draft.attachments.length && (draft.edit || draft.quote)) {
    if (!changes.attachments || !changes.attachments.length) draft = Common.withChanges(draft, { attachments: [] })
    else if (draft.edit) return state
    else draft = Common.withChanges(draft, { quote: "" })
  }
  var marks = (state.openChat.unsendable || []).filter(function(path) { return draft.attachments.indexOf(path) >= 0 })
  if (draft.text === old.text && draft.quote === old.quote && draft.edit === old.edit && sameList(draft.attachments, old.attachments) &&
      marks.length === (state.openChat.unsendable || []).length) return state
  return Common.withChanges(state, { openChat: Common.withChanges(state.openChat, { draft: draft, unsendable: marks }) })
}

function clearDraft(state) {
  return changeDraft(state, EMPTY_DRAFT)
}

// The composer sent the Draft as request `id`: clear the view Draft at once, and keep the sent one in case the Daemon refuses the send.
// Only a send with Attachments is kept, because only that Draft is worth a restore.
function submitDraft(state, id) {
  var draft = draftOf(state)
  var sending = draft.attachments.length ? { id: id, jid: state.openChat.jid, draft: draft } : null
  return Common.withChanges(clearDraft(state), { sending: sending })
}

// The answer to the send of submitDraft. After an error the Daemon still stores the Draft, so the view takes it back:
// the sent Attachments, marked where error.data.paths names them, and the sent text unless the user typed new text since.
function sendAnswered(state, frame) {
  var sent = state.sending
  var next = Common.withChanges(state, { sending: null })
  if (!frame.error || !next.openChat || next.openChat.jid !== sent.jid) return next
  var typed = draftOf(next).text
  next = changeDraft(next, { text: typed !== "" ? typed : sent.draft.text, attachments: sent.draft.attachments })
  var bad = (frame.error.data && frame.error.data.paths) || []
  return Common.withChanges(next, { openChat: Common.withChanges(next.openChat, { unsendable: bad }) })
}

// The Message that the Draft quotes (or edits, with `field` "edit"), if the Plugin has it loaded. Else null.
function draftTarget(state, field) {
  var id = draftOf(state)[field]
  var messages = state.openChat ? state.openChat.messages : []
  return id && messages.filter(function(m) { return m.id === id })[0] || null
}

function draftGetRequest(id, jid) {
  return Common.request(id, "draft.get", { chat: jid })
}

function draftSetRequest(id, jid, draft) {
  return Common.request(id, "draft.set", { chat: jid, text: draft.text, quote: draft.quote, edit: draft.edit, attachments: draft.attachments })
}

// `quoteId` is the Message that the new Message quotes. Leave it out for a plain Message.
function sendRequest(id, jid, text, quoteId) {
  var params = { chat: jid, text: text }
  if (quoteId) params.quote = quoteId
  return Common.request(id, "chat.send", params)
}

// Whether a paste can give an Attachment: not during an Edit, which never combines with one.
function canAttach(editingId) {
  return editingId === ""
}

// The request that reads the clipboard for a Ctrl+V in the composer.
function pasteRequest(id, jid) {
  return Common.request(id, "draft.paste", { chat: jid })
}

// What a draft.paste reply gives the open Chat: the pasted paths, [] to paste text, or null for any other frame.
function pastedAttachment(state, frame) {
  if (!frame || !frame.result || !Array.isArray(frame.result.pasted)) return null
  if (!state.openChat || state.openChat.jid !== frame.result.chat) return null
  return frame.result.pasted
}

// A draft.paste reply with files and text asks the user first: the state keeps the files until the answer.
function pasteAsked(state, paths) {
  return Common.withChanges(state, { pasteChoice: { jid: state.openChat.jid, paths: paths } })
}

function pasteAnswered(state) {
  return Common.withChanges(state, { pasteChoice: null })
}

// The question of the open Chat about the pasted files, or null: { paths, text }. A switch of Chat drops it.
// Each folder shows in full, with "~" for `home`, so a file from a hidden folder is plain to see.
function pasteQuestion(state, home) {
  var choice = state.pasteChoice
  if (!choice || !state.openChat || state.openChat.jid !== choice.jid) return null
  var folders = []
  choice.paths.forEach(function(path) {
    var dir = path.substring(0, path.lastIndexOf("/")) || "/"
    if (home && (dir === home || dir.indexOf(home + "/") === 0)) dir = "~" + dir.substring(home.length)
    if (folders.indexOf(dir) < 0) folders.push(dir)
  })
  var n = choice.paths.length
  var from = folders.length === 1 ? folders[0] : folders.length + " folders: " + folders.join(", ")
  return { paths: choice.paths, text: "Attach " + n + (n === 1 ? " file" : " files") + " from " + from + "?" }
}

// True while a question waits: the paste question, or the Open question (`confirmOpen`, or null).
function questionOpen(state, confirmOpen) {
  return confirmOpen !== null || pasteQuestion(state) !== null
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
  return Common.request(id, "chat.sendMedia", { chat: jid, paths: paths, caption: caption })
}

// What the composer does with a key press: "send", "swallow" (Enter with nothing to send), "paste" (ask the Daemon)
// or "default" (the text field handles it). Enter sends and Shift+Enter adds a line. These keybindings are fixed.
// An Attachment can go without text. During an Edit, Ctrl+V pastes text in the text field.
function composerAction(key, shift, text, hasAttachment, ctrl, editingId) {
  if (ctrl && key === KEY_V) return canAttach(editingId || "") ? "paste" : "default"
  if (key !== Common.KEY_RETURN && key !== Common.KEY_ENTER) return "default"
  if (shift) return "default"
  return canSend(text, hasAttachment) ? "send" : "swallow"
}

// A send needs text or an attached file. Enter and the send button share this rule.
function canSend(text, hasAttachment) {
  return String(text).trim() !== "" || !!hasAttachment
}

// The request for the composer text: an edit of `editingId`, or a new Message when `editingId` is empty.
// A new Message quotes `quoteId` when it is set.
// Attachments (`attachments`, a list of paths) go as Media with the text as caption. The Draft never holds a Quote with files.
function composerRequest(id, jid, editingId, text, quoteId, attachments) {
  if (editingId) return Message.editRequest(id, jid, editingId, text)
  return attachments && attachments.length ? sendMediaRequest(id, jid, attachments, text) : sendRequest(id, jid, text, quoteId)
}

// What the composer text means for the outgoing Composing: "start", "stop" or "" (no change).
// `draftText` is the stored Draft text before this change. Text equal to it came from the Draft
// (a load, a restore or an Edit), not from the user, so it never starts Composing.
function typingAction(wasTyping, text, draftText) {
  var typing = String(text).trim() !== ""
  if (typing === wasTyping) return ""
  if (typing && text === draftText) return ""
  return typing ? "start" : "stop"
}

function typingRequest(id, jid, typing) {
  return Common.request(id, "chat.typing", { chat: jid, typing: typing })
}
