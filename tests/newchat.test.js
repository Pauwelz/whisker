const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Daemon = load("state/Daemon.js");
const NewChat = load("state/NewChat.js");
const Timeline = load("state/Timeline.js");

const ann = { jid: "1@s.whatsapp.net", name: "Ann", phone: "+1", pushNameOnly: false, chat: "1@s.whatsapp.net" };
const bob = { jid: "2@s.whatsapp.net", name: "Bob", phone: "+2", pushNameOnly: false, chat: "" };
const cy = { jid: "3@lid", name: "Cy", phone: "", pushNameOnly: true, chat: "" };

test("newChatRows labels a Phone Contact with a Chat 'open' and one without 'new'", () => {
  const rows = NewChat.newChatRows([ann, bob]);
  assert.deepEqual(Array.from(rows.slice(0, 2), (r) => [r.jid, r.label]), [[ann.jid, "open"], [bob.jid, "new"]]);
});

test("newChatRows keeps the order of the Daemon and ends with the Group Chat note", () => {
  const rows = NewChat.newChatRows([ann, bob, cy]);
  assert.deepEqual(Array.from(rows, (r) => r.kind), ["contact", "contact", "contact", "note"]);
  assert.equal(rows[2].jid, cy.jid);
  assert.equal(rows[2].pushNameOnly, true);
  assert.match(rows[3].text, /group chats are created on the phone/i);
});

test("newChatRows with no people gives the note only", () => {
  assert.deepEqual(Array.from(NewChat.newChatRows([]), (r) => r.kind), ["note"]);
});

test("contacts.search and chat.start build requests", () => {
  assert.deepEqual(JSON.parse(NewChat.contactsSearchRequest(4, "an")), { jsonrpc: "2.0", id: 4, method: "contacts.search", params: { query: "an" } });
  assert.deepEqual(JSON.parse(NewChat.chatStartRequest(5, ann.jid)), { jsonrpc: "2.0", id: 5, method: "chat.start", params: { jid: ann.jid } });
});

test("a chat.start result changes nothing", () => {
  const state = Daemon.initialState();
  assert.equal(Daemon.applyFrame(state, { id: 1, result: { chat: { jid: ann.jid, kind: "direct", name: "Ann" } } }), state);
});

test("panelParts shows the new Chat view in place of the Chat list", () => {
  const linked = { screen: Daemon.SCREEN.LINKED, moreChats: true };
  const list = Daemon.panelParts(linked, { showNewChat: false });
  const view = Daemon.panelParts(linked, { showNewChat: true });
  assert.equal(list.chatList, true);
  assert.equal(list.newChat, false);
  assert.equal(view.newChat, true);
  assert.equal(view.chatList, false);
  assert.equal(view.loadMoreChats, false);
});

test("Escape closes the new Chat view before the pop-out", () => {
  const linked = { screen: Daemon.SCREEN.LINKED };
  assert.equal(Daemon.shortcutAction("Escape", linked, { showNewChat: true }), "closeNewChat");
  assert.equal(Daemon.shortcutAction("Escape", linked, {}), "closePopout");
});

test("the empty Chat text shows once the Messages loaded and there are none", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
  state = Timeline.openChat(state, { jid: ann.jid, name: "Ann", kind: "direct" });
  assert.equal(Daemon.panelParts(state, {}).emptyChat, false);
  state = Daemon.applyFrame(state, { id: 1, result: { chat: ann.jid, messages: [], more: false, offset: 0 } });
  assert.equal(Daemon.panelParts(state, {}).emptyChat, true);
  assert.equal(NewChat.EMPTY_CHAT_TEXT, "No messages yet. Your first message starts the chat.");
});

const found = { phone: "15551234567", status: "found", jid: "15551234567@s.whatsapp.net" };
const phoneRow = (query, lookup) => NewChat.newChatRows([ann], query, lookup)[0];

test("a search text that is not a phone number adds no phone row", () => {
  assert.deepEqual(Array.from(NewChat.newChatRows([ann], "an"), (r) => r.kind), ["contact", "note"]);
  assert.deepEqual(Array.from(NewChat.newChatRows([ann], ""), (r) => r.kind), ["contact", "note"]);
});

test("a phone number shows a phone row first, with separators dropped", () => {
  const row = phoneRow("+1 (555) 123-4567", found);
  assert.deepEqual([row.kind, row.phone, row.status, row.jid], ["phone", "+15551234567", "found", found.jid]);
  assert.equal(row.label, "on WhatsApp · message");
});

test("the phone row is checking until the reply for its number comes", () => {
  assert.equal(phoneRow("15551234567", undefined).label, "checking WhatsApp…");
  assert.equal(phoneRow("15551234567", { phone: "1555123456", status: "found", jid: "x@s.whatsapp.net" }).status, "checking");
  assert.equal(phoneRow("15551234567", { phone: "15551234567", status: "checking", jid: "" }).jid, "");
});

test("the phone row shows not on WhatsApp, offline and malformed", () => {
  assert.equal(phoneRow("15551234567", { phone: "15551234567", status: "none", jid: "" }).label, "not on WhatsApp");
  assert.equal(phoneRow("15551234567", { phone: "15551234567", status: "offline", jid: "" }).label, "Could not reach WhatsApp");
  assert.equal(phoneRow("12345").status, "malformed");
  assert.equal(phoneRow("0612345678").status, "malformed");
});

test("contacts.lookup builds a request", () => {
  assert.deepEqual(JSON.parse(NewChat.lookupRequest(7, "15551234567")), { jsonrpc: "2.0", id: 7, method: "contacts.lookup", params: { phone: "15551234567" } });
});

test("a late lookup reply for another number changes nothing", () => {
  const state = NewChat.lookupStarted(Daemon.initialState(), "15551234567");
  assert.equal(Daemon.applyFrame(state, { id: 1, result: { phone: "1555123456", jid: "x@s.whatsapp.net" } }), state);
  assert.equal(Daemon.applyFrame(Daemon.initialState(), { id: 1, result: { phone: "1555123456", jid: "" } }).lookup, undefined);
});

test("a could-not-reach error turns a checking lookup offline, and leaves a finished one", () => {
  const error = { id: 2, error: { code: -32007, message: "could not reach WhatsApp" } };
  const offline = Daemon.applyFrame(NewChat.lookupStarted(Daemon.initialState(), "15551234567"), error);
  assert.equal(offline.lookup.status, "offline");
  assert.equal(offline.notice, "Could not reach WhatsApp");
  const done = Daemon.applyFrame(Daemon.initialState(), error);
  assert.equal(done.lookup, undefined);
});

test("a lookup starts only for a full phone number", () => {
  // phone_test.go (TestCleanPhone) holds the same cases.
  assert.equal(NewChat.lookupPhone("+1 (555) 123-4567"), "15551234567");
  assert.equal(NewChat.lookupPhone("32470123456"), "32470123456");
  assert.equal(NewChat.lookupPhone("1234567"), "1234567");
  assert.equal(NewChat.lookupPhone("123456"), "");
  assert.equal(NewChat.lookupPhone("+0 32 470"), "");
  assert.equal(NewChat.lookupPhone("abc"), "");
  assert.equal(NewChat.lookupPhone("0612345678"), "");
  assert.equal(NewChat.lookupPhone("ann"), "");
  assert.equal(NewChat.lookupPhone(""), "");
});

test("newChatTarget names a phone row by its number and passes a contact row through", () => {
  const phoneRow = { kind: "phone", phone: "+15551234567", jid: "15551234567@s.whatsapp.net", status: "found" };
  const target = NewChat.newChatTarget(phoneRow);
  assert.equal(target.jid, phoneRow.jid);
  assert.equal(target.name, "+15551234567");
  const contactRow = NewChat.newChatRows([ann])[0];
  assert.equal(NewChat.newChatTarget(contactRow), contactRow);
});

test("newChatRowName is the number of a phone row and the name of a contact row", () => {
  assert.equal(NewChat.newChatRowName({ kind: "phone", phone: "+1555" }), "+1555");
  assert.equal(NewChat.newChatRowName({ kind: "contact", name: "Ann", phone: "+1" }), "Ann");
});
