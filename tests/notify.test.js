const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");

test("the bar counts unread Chats and leaves Muted Chats out", () => {
  const chats = [
    { jid: "1", unread: true, muted: false },
    { jid: "2", unread: true, muted: true },
    { jid: "3", unread: false, muted: false },
    { jid: "4", unread: true, muted: false },
  ];
  assert.equal(State.unreadCount({ chats }), 2);
  assert.equal(State.unreadLabel({ chats }), "2");
});

test("the bar shows only the icon when no Chat is unread", () => {
  assert.equal(State.unreadLabel({ chats: [{ jid: "1", unread: false, muted: false }] }), "");
  assert.equal(State.unreadLabel({}), "");
});

test("a Chat marked unread on the phone counts after chats.list", () => {
  const state = State.applyFrame(State.initialState(), { jsonrpc: "2.0", id: 1, result: { chats: [{ jid: "1", unread: true, muted: false }], more: false, offset: 0 } });
  assert.equal(State.unreadLabel(state), "1");
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
