const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");

const ann = { jid: "1@s.whatsapp.net", kind: "direct", name: "Ann", unread: true, unreadCount: 2 };
const fam = { jid: "g@g.us", kind: "group", name: "Family", unread: false };
const me = { jid: "9@s.whatsapp.net", kind: "self", name: "You", unread: false };

test("filterChats keeps all, the unread Chats or the Group Chats", () => {
  const chats = [ann, fam, me];
  assert.deepEqual(State.filterChats(chats, "all").map((c) => c.name), ["Ann", "Family", "You"]);
  assert.deepEqual(State.filterChats(chats, "unread").map((c) => c.name), ["Ann"]);
  assert.deepEqual(State.filterChats(chats, "groups").map((c) => c.name), ["Family"]);
});

test("chatSign tells Direct, Group and Self Chats apart", () => {
  const signs = [ann, fam, me].map(State.chatSign);
  assert.equal(new Set(signs).size, 3);
});

test("chatCountLabel shows the count, a dot for a phone mark, or nothing", () => {
  assert.equal(State.chatCountLabel(ann), "2");
  assert.equal(State.chatCountLabel({ unread: true }), "●");
  assert.equal(State.chatCountLabel(fam), "");
  assert.equal(State.chatCountLabel({ unread: true, unreadCount: 250 }), "99+");
});

test("chatActivityLabel reads the activity of any listed Chat", () => {
  let state = State.applyFrame(State.initialState(), { method: "activityChanged", params: { chat: "g@g.us", participant: "2", name: "Bob", state: "composing" } });
  state = State.applyFrame(state, { method: "activityChanged", params: { chat: ann.jid, participant: ann.jid, name: "Ann", state: "recording" } });
  assert.equal(State.chatActivityLabel(state, fam), "Bob is typing…");
  assert.equal(State.chatActivityLabel(state, ann), "recording…");
  assert.equal(State.chatActivityLabel(state, me), "");
});

test("chatTimeLabel shows the clock today and the date before", () => {
  const now = new Date(2026, 5, 15, 12, 0).getTime() / 1000;
  const at = (y, mo, d, h, mi) => new Date(y, mo, d, h, mi).getTime() / 1000;
  assert.equal(State.chatTimeLabel(at(2026, 5, 15, 9, 5), now), "09:05");
  assert.equal(State.chatTimeLabel(at(2026, 5, 14, 23, 0), now), "14 Jun");
  assert.equal(State.chatTimeLabel(0, now), "");
});

test("emptyChatsText tells why no Chat shows", () => {
  assert.equal(State.emptyChatsText("all", ""), "No chats yet.");
  assert.equal(State.emptyChatsText("unread", ""), "No unread chats.");
  assert.equal(State.emptyChatsText("groups", ""), "No group chats.");
  assert.equal(State.emptyChatsText("all", "zed"), "No chat matches “zed”.");
});
