const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const ChatList = load("state/ChatList.js");
const Daemon = load("state/Daemon.js");

const ann = { jid: "1@s.whatsapp.net", kind: "direct", name: "Ann", unread: true, unreadCount: 2 };
const fam = { jid: "g@g.us", kind: "group", name: "Family", unread: false };
const me = { jid: "9@s.whatsapp.net", kind: "self", name: "You", unread: false };

test("filterChats keeps all, the unread Chats or the Group Chats", () => {
  const chats = [ann, fam, me];
  assert.deepEqual(ChatList.filterChats(chats, "all").map((c) => c.name), ["Ann", "Family", "You"]);
  assert.deepEqual(ChatList.filterChats(chats, "unread").map((c) => c.name), ["Ann"]);
  assert.deepEqual(ChatList.filterChats(chats, "groups").map((c) => c.name), ["Family"]);
  assert.deepEqual(ChatList.filterChats(chats, "archived").map((c) => c.name), ["Ann", "Family", "You"]); // the Daemon filtered
});

test("chatSign tells Direct, Group and Self Chats apart", () => {
  const signs = [ann, fam, me].map(ChatList.chatSign);
  assert.equal(new Set(signs).size, 3);
});

test("chatCountLabel shows the count, a dot for a phone mark, or nothing", () => {
  assert.equal(ChatList.chatCountLabel(ann), "2");
  assert.equal(ChatList.chatCountLabel({ unread: true }), "●");
  assert.equal(ChatList.chatCountLabel(fam), "");
  assert.equal(ChatList.chatCountLabel({ unread: true, unreadCount: 250 }), "99+");
});

test("chatActivityLabel reads the activity of any listed Chat", () => {
  let state = Daemon.applyFrame(Daemon.initialState(), { method: "activityChanged", params: { chat: "g@g.us", participant: "2", name: "Bob", state: "composing" } });
  state = Daemon.applyFrame(state, { method: "activityChanged", params: { chat: ann.jid, participant: ann.jid, name: "Ann", state: "recording" } });
  assert.equal(ChatList.chatActivityLabel(state, fam), "Bob is typing…");
  assert.equal(ChatList.chatActivityLabel(state, ann), "recording…");
  assert.equal(ChatList.chatActivityLabel(state, me), "");
});

test("chatTimeLabel shows the clock today and the date before", () => {
  const now = new Date(2026, 5, 15, 12, 0).getTime() / 1000;
  const at = (y, mo, d, h, mi) => new Date(y, mo, d, h, mi).getTime() / 1000;
  assert.equal(ChatList.chatTimeLabel(at(2026, 5, 15, 9, 5), now), "09:05");
  assert.equal(ChatList.chatTimeLabel(at(2026, 5, 14, 23, 0), now), "14 Jun");
  assert.equal(ChatList.chatTimeLabel(0, now), "");
});

test("emptyChatsText tells why no Chat shows", () => {
  assert.equal(ChatList.emptyChatsText("all", ""), "No chats yet.");
  assert.equal(ChatList.emptyChatsText("unread", ""), "No unread chats.");
  assert.equal(ChatList.emptyChatsText("groups", ""), "No group chats.");
  assert.equal(ChatList.emptyChatsText("archived", ""), "No archived chats.");
  assert.equal(ChatList.emptyChatsText("all", "zed"), "No chat matches “zed”.");
});
