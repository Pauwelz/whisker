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

const texts = (options) => plain(options.map((o) => o.text));
const option = (options, text) => plain(options.find((o) => o.text === text).option);

test("an unmuted Chat offers pin, mark as unread, and mute with the three choices", () => {
  const options = State.chatOptions(ann, NOW);
  assert.deepEqual(texts(options), ["pin", "mark as unread", "mute", "8h", "1w", "always"]);
  assert.deepEqual(option(options, "pin"), { pinned: true });
  assert.equal(option(options, "mute"), null); // a label, not a choice
  assert.deepEqual(plain(options.slice(3).map((o) => o.option.until)), [NOW + 28800, NOW + 604800, -1]);
});

test("a pinned and muted Chat offers unpin and unmute with the time left", () => {
  const hours = State.chatOptions({ ...ann, pinned: true, muted: true, mutedUntil: NOW + 3 * 3600 + 5 }, NOW);
  assert.deepEqual(texts(hours), ["unpin", "mark as unread", "unmute (3h left)"]);
  assert.deepEqual(option(hours, "unpin"), { pinned: false });
  assert.deepEqual(option(hours, "unmute (3h left)"), { until: 0 });
  assert.equal(State.chatOptions({ ...ann, muted: true, mutedUntil: -1 }, NOW)[2].text, "unmute (for ever)");
  assert.equal(State.chatOptions({ ...ann, muted: true, mutedUntil: NOW + 2 * 86400 }, NOW)[2].text, "unmute (2d left)");
  assert.equal(State.chatOptions({ ...ann, muted: true, mutedUntil: NOW + 10 }, NOW)[2].text, "unmute (1m left)");
});

test("a choice becomes a pin or a mute request", () => {
  const options = State.chatOptions(ann, NOW);
  assert.deepEqual(JSON.parse(State.chatOptionRequest(4, ann.jid, option(options, "pin"))),
    { jsonrpc: "2.0", id: 4, method: "chat.pin", params: { chat: ann.jid, pinned: true } });
  assert.deepEqual(JSON.parse(State.chatOptionRequest(5, ann.jid, option(options, "8h"))),
    { jsonrpc: "2.0", id: 5, method: "chat.mute", params: { chat: ann.jid, until: NOW + 28800 } });
  assert.deepEqual(JSON.parse(State.chatOptionRequest(6, ann.jid, State.UNMUTE)).params, { chat: ann.jid, until: 0 });
});

test("the menu offers mark as read on an unread Chat, and both marks make a markUnread request", () => {
  const unread = option(State.chatOptions(ann, NOW), "mark as unread");
  assert.deepEqual(JSON.parse(State.chatOptionRequest(7, ann.jid, unread)),
    { jsonrpc: "2.0", id: 7, method: "chat.markUnread", params: { chat: ann.jid, unread: true } });
  const read = option(State.chatOptions({ ...ann, unread: true }, NOW), "mark as read");
  assert.equal(JSON.parse(State.chatOptionRequest(8, ann.jid, read)).params.unread, false);
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

test("the inline mute choices of a Chat line are short and end at the chosen time", () => {
  assert.deepEqual(plain(State.chatRowMuteChoices(NOW)),
    [{ text: "8h", option: { until: NOW + 28800 } }, { text: "1w", option: { until: NOW + 604800 } }, { text: "always", option: { until: -1 } }]);
});

test("the hover buttons of a Chat line: pin toggles, mute unmutes or shows the mute choices", () => {
  const buttons = (chat) => plain(State.chatRowButtons(chat));
  assert.deepEqual(buttons(ann), { pin: { text: "pin", option: { pinned: true } }, mute: { text: "mute", option: null } });
  assert.deepEqual(buttons({ ...ann, pinned: true, muted: true }),
    { pin: { text: "unpin", option: { pinned: false } }, mute: { text: "unmute", option: { until: 0 } } });
});
