const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");

const page = (result) => ({ jsonrpc: "2.0", id: 1, result: Object.assign({ chats: [], more: false, offset: 0 }, result) });

test("the bar shows the unread count from the Daemon, not from the loaded Chats", () => {
  const state = State.applyFrame(State.initialState(), page({ unreadChats: 7, chats: [{ jid: "1", unread: true, muted: false }] }));
  assert.equal(State.unreadCount(state), 7);
  assert.equal(State.unreadLabel(state), "7");
});

test("a search result or a later page does not change the bar count", () => {
  let state = State.applyFrame(State.initialState(), page({ unreadChats: 3 }));
  state = State.applyFrame(state, page({ unreadChats: 3, offset: 50, chats: [{ jid: "2", unread: false, muted: false }] }));
  assert.equal(State.unreadCount(state), 3);
});

test("the bar shows only the icon when no Chat is unread", () => {
  assert.equal(State.unreadLabel(State.applyFrame(State.initialState(), page({ unreadChats: 0 }))), "");
  assert.equal(State.unreadLabel({}), "");
});

test("a notify frame becomes a notification whose click opens the Chat", () => {
  const frame = { method: "notify", params: { chat: "g1@g.us", kind: "group", name: "Crew", senderName: "Bob", text: "hi" } };
  assert.equal(State.isNotify(frame), true);
  assert.deepEqual(JSON.parse(JSON.stringify(State.notifyCommand(frame.params))), [
    "omarchy-notification-send", "--app-name", "whisker", "Crew", "Bob: hi",
    "--exec", "omarchy-shell", "pauwelz.whisker", "openChat", "g1@g.us",
  ]);
  assert.equal(State.notifyCommand({ chat: "1@s.whatsapp.net", kind: "direct", name: "Ann", senderName: "Ann", text: "yo" })[4], "yo");
});

test("a headline that starts with a dash is not read as an option", () => {
  const argv = State.notifyCommand({ chat: "1", kind: "direct", name: "-g evil", senderName: "", text: "x" });
  assert.equal(argv[3].charAt(0), " ");
});

test("chatByJid finds a listed Chat and falls back to a minimal row", () => {
  const chats = [{ jid: "1", name: "Ann", kind: "direct" }];
  assert.equal(State.chatByJid({ chats }, "1").name, "Ann");
  assert.deepEqual(JSON.parse(JSON.stringify(State.chatByJid({ chats }, "9"))), { jid: "9", name: "9", kind: "direct" });
});

test("a Message with Media and no text is named by its Media", () => {
  const argv = State.notifyCommand({ chat: "1", kind: "direct", name: "Ann", senderName: "Ann", text: "", media: "voice" });
  assert.equal(argv[4], "Voice message");
});
