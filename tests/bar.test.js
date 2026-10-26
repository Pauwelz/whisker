const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const ChatList = load("state/ChatList.js");
const Daemon = load("state/Daemon.js");

const page = (result) => ({ jsonrpc: "2.0", id: 1, result: Object.assign({ chats: [], more: false, offset: 0 }, result) });

test("the bar shows the unread count from the Daemon, not from the loaded Chats", () => {
  const state = Daemon.applyFrame(Daemon.initialState(), page({ unreadChats: 7, chats: [{ jid: "1", unread: true, muted: false }] }));
  assert.equal(Daemon.unreadCount(state), 7);
  assert.equal(Daemon.unreadLabel(state), "7");
});

test("a search result or a later page does not change the bar count", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), page({ unreadChats: 3 }));
  state = Daemon.applyFrame(state, page({ unreadChats: 3, offset: 50, chats: [{ jid: "2", unread: false, muted: false }] }));
  assert.equal(Daemon.unreadCount(state), 3);
});

test("the bar shows only the icon when no Chat is unread", () => {
  assert.equal(Daemon.unreadLabel(Daemon.applyFrame(Daemon.initialState(), page({ unreadChats: 0 }))), "");
  assert.equal(Daemon.unreadLabel({}), "");
});

test("chatByJid finds a listed Chat and falls back to a minimal row", () => {
  const chats = [{ jid: "1", name: "Ann", kind: "direct" }];
  assert.equal(ChatList.chatByJid({ chats }, "1").name, "Ann");
  assert.deepEqual(JSON.parse(JSON.stringify(ChatList.chatByJid({ chats }, "9"))), { jid: "9", name: "9", kind: "direct" });
});
