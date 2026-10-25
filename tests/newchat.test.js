const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");

const ann = { jid: "1@s.whatsapp.net", name: "Ann", phone: "+1", pushNameOnly: false, chat: "1@s.whatsapp.net" };
const bob = { jid: "2@s.whatsapp.net", name: "Bob", phone: "+2", pushNameOnly: false, chat: "" };
const cy = { jid: "3@lid", name: "Cy", phone: "", pushNameOnly: true, chat: "" };

test("newChatRows labels a Phone Contact with a Chat 'open' and one without 'new'", () => {
  const rows = State.newChatRows([ann, bob]);
  assert.deepEqual(Array.from(rows.slice(0, 2), (r) => [r.jid, r.label]), [[ann.jid, "open"], [bob.jid, "new"]]);
});

test("newChatRows keeps the order of the Daemon and ends with the Group Chat note", () => {
  const rows = State.newChatRows([ann, bob, cy]);
  assert.deepEqual(Array.from(rows, (r) => r.kind), ["contact", "contact", "contact", "note"]);
  assert.equal(rows[2].jid, cy.jid);
  assert.equal(rows[2].pushNameOnly, true);
  assert.match(rows[3].text, /group chats are created on the phone/i);
});

test("newChatRows with no people gives the note only", () => {
  assert.deepEqual(Array.from(State.newChatRows([]), (r) => r.kind), ["note"]);
});

test("contacts.search and chat.start build requests", () => {
  assert.deepEqual(JSON.parse(State.contactsSearchRequest(4, "an")), { jsonrpc: "2.0", id: 4, method: "contacts.search", params: { query: "an" } });
  assert.deepEqual(JSON.parse(State.chatStartRequest(5, ann.jid)), { jsonrpc: "2.0", id: 5, method: "chat.start", params: { jid: ann.jid } });
});

test("a chat.start result changes nothing", () => {
  const state = State.initialState();
  assert.equal(State.applyFrame(state, { id: 1, result: { chat: { jid: ann.jid, kind: "direct", name: "Ann" } } }), state);
});

test("panelParts shows the new Chat view in place of the Chat list", () => {
  const linked = { screen: State.SCREEN.LINKED, moreChats: true };
  const list = State.panelParts(linked, { showNewChat: false });
  const view = State.panelParts(linked, { showNewChat: true });
  assert.equal(list.chatList, true);
  assert.equal(list.newChat, false);
  assert.equal(view.newChat, true);
  assert.equal(view.chatList, false);
  assert.equal(view.loadMoreChats, false);
});

test("Escape closes the new Chat view before the pop-out", () => {
  const linked = { screen: State.SCREEN.LINKED };
  assert.equal(State.shortcutAction("Escape", linked, { showNewChat: true }), "closeNewChat");
  assert.equal(State.shortcutAction("Escape", linked, {}), "closePopout");
});

test("the empty Chat text shows once the Messages loaded and there are none", () => {
  let state = State.applyFrame(State.initialState(), { method: "linked" });
  state = State.openChat(state, { jid: ann.jid, name: "Ann", kind: "direct" });
  assert.equal(State.panelParts(state, {}).emptyChat, false);
  state = State.applyFrame(state, { id: 1, result: { chat: ann.jid, messages: [], more: false, offset: 0 } });
  assert.equal(State.panelParts(state, {}).emptyChat, true);
  assert.equal(State.EMPTY_CHAT_TEXT, "No messages yet. Your first message starts the chat.");
});

const found = { phone: "15551234567", status: "found", jid: "15551234567@s.whatsapp.net" };
const phoneRow = (query, lookup) => State.newChatRows([ann], query, lookup)[0];

test("a search text that is not a phone number adds no phone row", () => {
  assert.deepEqual(Array.from(State.newChatRows([ann], "an"), (r) => r.kind), ["contact", "note"]);
  assert.deepEqual(Array.from(State.newChatRows([ann], ""), (r) => r.kind), ["contact", "note"]);
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
  assert.deepEqual(JSON.parse(State.lookupRequest(7, "15551234567")), { jsonrpc: "2.0", id: 7, method: "contacts.lookup", params: { phone: "15551234567" } });
});

test("a late lookup reply for another number changes nothing", () => {
  const state = State.lookupStarted(State.initialState(), "15551234567");
  assert.equal(State.applyFrame(state, { id: 1, result: { phone: "1555123456", jid: "x@s.whatsapp.net" } }), state);
  assert.equal(State.applyFrame(State.initialState(), { id: 1, result: { phone: "1555123456", jid: "" } }).lookup, undefined);
});

test("a could-not-reach error turns a checking lookup offline, and leaves a finished one", () => {
  const error = { id: 2, error: { code: -32007, message: "could not reach WhatsApp" } };
  const offline = State.applyFrame(State.lookupStarted(State.initialState(), "15551234567"), error);
  assert.equal(offline.lookup.status, "offline");
  assert.equal(offline.notice, "Could not reach WhatsApp");
  const done = State.applyFrame(State.initialState(), error);
  assert.equal(done.lookup, undefined);
});

test("a lookup starts only for a full phone number", () => {
  // phone_test.go (TestCleanPhone) holds the same cases.
  assert.equal(State.lookupPhone("+1 (555) 123-4567"), "15551234567");
  assert.equal(State.lookupPhone("32470123456"), "32470123456");
  assert.equal(State.lookupPhone("1234567"), "1234567");
  assert.equal(State.lookupPhone("123456"), "");
  assert.equal(State.lookupPhone("+0 32 470"), "");
  assert.equal(State.lookupPhone("abc"), "");
  assert.equal(State.lookupPhone("0612345678"), "");
  assert.equal(State.lookupPhone("ann"), "");
  assert.equal(State.lookupPhone(""), "");
});

test("newChatTarget names a phone row by its number and passes a contact row through", () => {
  const phoneRow = { kind: "phone", phone: "+15551234567", jid: "15551234567@s.whatsapp.net", status: "found" };
  const target = State.newChatTarget(phoneRow);
  assert.equal(target.jid, phoneRow.jid);
  assert.equal(target.name, "+15551234567");
  const contactRow = State.newChatRows([ann])[0];
  assert.equal(State.newChatTarget(contactRow), contactRow);
});

test("newChatRowName is the number of a phone row and the name of a contact row", () => {
  assert.equal(State.newChatRowName({ kind: "phone", phone: "+1555" }), "+1555");
  assert.equal(State.newChatRowName({ kind: "contact", name: "Ann", phone: "+1" }), "Ann");
});
