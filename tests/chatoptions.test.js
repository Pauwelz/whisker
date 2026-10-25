const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const load = require("./load");

const State = load("WhiskerState.js");

const NOW = 1000;
const plain = (x) => JSON.parse(JSON.stringify(x)); // a vm array has another prototype
const ann = { jid: "1@s.whatsapp.net", kind: "direct", name: "Ann", pinned: false, muted: false };

test("muteEnd turns a choice into an absolute end time", () => {
  const [eight, week, always] = State.MUTE_CHOICES;
  assert.equal(State.muteEnd(eight, NOW), NOW + 8 * 3600);
  assert.equal(State.muteEnd(week, NOW), NOW + 7 * 86400);
  assert.equal(State.muteEnd(always, NOW), -1);
});

const texts = (links) => plain(links.map((o) => o.text));
const option = (links, text) => plain(links.find((o) => o.text === text).option);

test("the links of a Chat: pin, mark unread and mute", () => {
  const links = State.chatLinks(ann, false, NOW);
  assert.deepEqual(texts(links), ["pin", "mark unread", "mute"]);
  assert.deepEqual(option(links, "pin"), { pinned: true });
  assert.deepEqual(option(links, "mark unread"), { unread: true });
  assert.equal(option(links, "mute"), null); // shows the mute choices
});

test("the links of a pinned, unread and muted Chat: unpin, mark read and unmute", () => {
  const links = State.chatLinks({ ...ann, pinned: true, unread: true, muted: true }, false, NOW);
  assert.deepEqual(texts(links), ["unpin", "mark read", "unmute"]);
  assert.deepEqual(option(links, "unpin"), { pinned: false });
  assert.deepEqual(option(links, "mark read"), { unread: false });
  assert.deepEqual(option(links, "unmute"), { until: 0 });
});

test("after a click on mute, the links are the mute choices with their end times", () => {
  assert.deepEqual(plain(State.chatLinks(ann, true, NOW)),
    [{ text: "8h", option: { until: NOW + 28800 } }, { text: "1w", option: { until: NOW + 604800 } }, { text: "always", option: { until: -1 } }]);
});

test("the time left of a Muted Chat", () => {
  const left = (mutedUntil) => State.muteLeftLabel({ ...ann, muted: true, mutedUntil }, NOW);
  assert.equal(left(-1), "for ever");
  assert.equal(left(NOW + 2 * 86400), "2d left");
  assert.equal(left(NOW + 3 * 3600 + 5), "3h left");
  assert.equal(left(NOW + 10), "1m left");
});

test("a link becomes a pin, markUnread or mute request", () => {
  const links = State.chatLinks(ann, false, NOW);
  assert.deepEqual(JSON.parse(State.chatOptionRequest(4, ann.jid, option(links, "pin"))),
    { jsonrpc: "2.0", id: 4, method: "chat.pin", params: { chat: ann.jid, pinned: true } });
  assert.deepEqual(JSON.parse(State.chatOptionRequest(5, ann.jid, option(State.chatLinks(ann, true, NOW), "8h"))),
    { jsonrpc: "2.0", id: 5, method: "chat.mute", params: { chat: ann.jid, until: NOW + 28800 } });
  assert.deepEqual(JSON.parse(State.chatOptionRequest(6, ann.jid, State.UNMUTE)).params, { chat: ann.jid, until: 0 });
  assert.deepEqual(JSON.parse(State.chatOptionRequest(7, ann.jid, option(links, "mark unread"))),
    { jsonrpc: "2.0", id: 7, method: "chat.markUnread", params: { chat: ann.jid, unread: true } });
});

test("only Mark as unread on the open Chat leaves the Chat", () => {
  const open = { openChat: { jid: ann.jid } };
  assert.equal(State.leavesChatFor(open, ann.jid, { unread: true }), true);
  assert.equal(State.leavesChatFor(open, ann.jid, { unread: false }), false);
  assert.equal(State.leavesChatFor(open, ann.jid, { pinned: true }), false);
  assert.equal(State.leavesChatFor(open, "x@s.whatsapp.net", { unread: true }), false);
  assert.equal(State.leavesChatFor({}, ann.jid, { unread: true }), false);
});

test("chatRow finds the listed Chat of a jid", () => {
  const state = { chats: [ann] };
  assert.equal(State.chatRow(state, ann.jid), ann);
  assert.equal(State.chatRow(state, "x@s.whatsapp.net"), null);
  assert.equal(State.chatRow({}, ann.jid), null);
});

test("error codes reach the view as a notice, not as text from the Daemon", () => {
  const state = State.initialState();
  const notice = (code, message) => State.applyFrame(state, { jsonrpc: "2.0", id: 1, error: { code, message } }).notice;
  assert.equal(notice(-32007, "anything"), "Could not reach WhatsApp");
  assert.equal(notice(-32011, "anything"), "You can pin up to 3 chats");
  assert.equal(notice(-32010, "anything"), "That chat is not in your chat list");
  assert.equal(notice(-32006, "invalid message"), undefined);
});

test("a notice keeps the rest of the state and can be cleared", () => {
  const state = State.applyFrame(State.initialState(), { id: 1, error: { code: -32007, message: "x" } });
  assert.equal(state.screen, State.initialState().screen);
  assert.equal(State.clearNotice(state).notice, "");
});

test("the contract doc lists the error codes of the Chat options", () => {
  const doc = fs.readFileSync(path.join(__dirname, "..", "docs", "contract.md"), "utf8");
  for (const code of ["-32007", "-32010", "-32011", "-32602"]) assert.ok(doc.includes(`| ${code} |`), code);
  for (const code of Object.keys(State.ERROR_NOTICES)) assert.ok(doc.includes(`| ${code} |`), code);
});

test("archivedFilterChanged is true only when the filter moves to or from archived", () => {
  assert.equal(State.archivedFilterChanged("all", "archived"), true);
  assert.equal(State.archivedFilterChanged("archived", "unread"), true);
  assert.equal(State.archivedFilterChanged("all", "unread"), false);
  assert.equal(State.archivedFilterChanged("archived", "archived"), false);
});
