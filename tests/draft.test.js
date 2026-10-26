const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Daemon = load("state/Daemon.js");
const Draft = load("state/Draft.js");
const Message = load("state/Message.js");
const Timeline = load("state/Timeline.js");
const chat = { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" };
const message = { id: "in1", chat: chat.jid, text: "lunch?", time: 1 };

function opened() {
  return Timeline.upsertMessage(Timeline.openChat(Daemon.initialState(), chat), message);
}

test("changing the Draft keeps the pending Quote and Edit target", () => {
  let state = Draft.changeDraft(opened(), { quote: "in1" });
  state = Draft.changeDraft(state, { text: "yes" });
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.draftOf(state))), { text: "yes", quote: "in1", edit: "", attachments: [] });
  assert.equal(Draft.draftTarget(state, "quote").id, "in1");
  assert.equal(Draft.draftTarget(state, "edit"), null);
});

// The Panel rebuilds its timeline only when the messages array changes.
test("changing the Draft keeps the messages array of the open Chat", () => {
  const state = opened();
  assert.equal(Draft.changeDraft(state, { text: "typed" }).openChat.messages, state.openChat.messages);
});

test("a change that changes nothing returns the same state", () => {
  const state = opened();
  assert.equal(Draft.changeDraft(state, { text: "" }), state);
});

test("a loaded Draft does not overwrite text that the user already typed", () => {
  let state = Draft.changeDraft(opened(), { text: "typed" });
  state = Daemon.applyFrame(state, { id: 1, result: { chat: chat.jid, draft: { text: "old", quote: "", edit: "" } } });
  assert.equal(Draft.draftOf(state).text, "typed");
});

test("a Draft of another Chat is dropped", () => {
  const state = Daemon.applyFrame(opened(), { id: 1, result: { chat: "2@s.whatsapp.net", draft: { text: "x", quote: "", edit: "" } } });
  assert.equal(Draft.draftOf(state).text, "");
});

test("the Draft requests carry the Chat and the Draft", () => {
  assert.deepEqual(JSON.parse(Draft.draftSetRequest(5, chat.jid, { text: "a", quote: "q", edit: "", attachments: ["/a/b.png", "/c.pdf"] })).params, { chat: chat.jid, text: "a", quote: "q", edit: "", attachments: ["/a/b.png", "/c.pdf"] });
  assert.equal(JSON.parse(Draft.draftGetRequest(6, chat.jid)).method, "draft.get");
});

function plain(state) {
  return JSON.parse(JSON.stringify(Draft.draftOf(state)));
}

test("Attachments are part of the Draft, in order, and keep the text", () => {
  let state = Draft.changeDraft(opened(), { text: "look" });
  state = Draft.changeDraft(state, { attachments: ["/home/me/photo.png", "/home/me/b.pdf"] });
  assert.deepEqual(Draft.attachmentRows(state).map((r) => r.label), ["Attached: photo.png", "Attached: b.pdf"]);
  state = Draft.changeDraft(state, { attachments: [] });
  assert.deepEqual(plain(state), { text: "look", quote: "", edit: "", attachments: [] });
});

test("a paste adds to the end and skips a path that is already there", () => {
  const draft = { attachments: ["/a", "/b"] };
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.appendAttachments(draft, ["/b", "/c", "/c"]))), ["/a", "/b", "/c"]);
});

test("removing one row keeps the other Attachments", () => {
  let state = Draft.changeDraft(opened(), { attachments: ["/a", "/b", "/c"] });
  state = Draft.changeDraft(state, { attachments: ["/a", "/c"] });
  assert.deepEqual(plain(state).attachments, ["/a", "/c"]);
});

test("starting an Edit drops the Attachments", () => {
  let state = Draft.changeDraft(opened(), { attachments: ["/a/b.png"] });
  state = Draft.changeDraft(state, { edit: "in1", text: "lunch?" });
  assert.deepEqual(plain(state).attachments, []);
  assert.equal(Draft.draftOf(state).edit, "in1");
});

test("adding Attachments during an Edit is refused", () => {
  const state = Draft.changeDraft(opened(), { edit: "in1", text: "lunch?" });
  assert.equal(Draft.changeDraft(state, { attachments: ["/a/b.png"] }), state);
});

test("adding Attachments drops the Quote, and starting a Quote drops the files", () => {
  let state = Draft.changeDraft(opened(), { quote: "in1", text: "x" });
  state = Draft.changeDraft(state, { attachments: ["/a/b.png"] });
  assert.deepEqual(plain(state), { text: "x", quote: "", edit: "", attachments: ["/a/b.png"] });
  state = Draft.changeDraft(state, { quote: "in1" });
  assert.deepEqual(plain(state), { text: "x", quote: "in1", edit: "", attachments: [] });
});

test("Attachments send as Media with the text as caption", () => {
  const sent = JSON.parse(Draft.composerRequest(1, chat.jid, "", "look", "", ["/a/b.png", "/c.pdf"]));
  assert.equal(sent.method, "chat.sendMedia");
  assert.deepEqual(sent.params, { chat: chat.jid, paths: ["/a/b.png", "/c.pdf"], caption: "look" });
  assert.equal(JSON.parse(Draft.composerRequest(2, chat.jid, "", "hi", "", [])).method, "chat.send");
});

test("clearing the Draft clears the Attachments", () => {
  const state = Draft.clearDraft(Draft.changeDraft(opened(), { attachments: ["/a/b.png"], text: "x" }));
  assert.deepEqual(plain(state).attachments, []);
});

test("a paste can attach files except during an Edit, and the rows show only with files", () => {
  assert.equal(Draft.canAttach(""), true);
  assert.equal(Draft.canAttach("in1"), false);
  assert.equal(Draft.hasAttachment({ attachments: ["/a/b.png"] }), true);
  assert.equal(Draft.hasAttachment(Draft.draftOf(opened())), false);
});

test("a loaded Draft marks the paths that the Daemon lists as unsendable", () => {
  const state = Daemon.applyFrame(opened(), { id: 1, result: { chat: chat.jid, draft: { text: "", quote: "", edit: "", attachments: ["/a", "/b"] }, unsendable: ["/b"] } });
  assert.deepEqual(Draft.attachmentRows(state).map((r) => [r.label, r.bad]), [["Attached: a", false], ["Not sendable: b", true]]);
});

test("removing a marked row removes its mark", () => {
  let state = Daemon.applyFrame(opened(), { id: 1, result: { chat: chat.jid, draft: { text: "", quote: "", edit: "", attachments: ["/a", "/b"] }, unsendable: ["/b"] } });
  state = Draft.changeDraft(state, { attachments: ["/a"] });
  state = Draft.changeDraft(state, { attachments: ["/a", "/b"] });
  assert.deepEqual(Draft.attachmentRows(state).map((r) => r.bad), [false, false]);
});

function sentWith(text) {
  let state = Draft.changeDraft(opened(), { text: text, attachments: ["/a", "/b"] });
  state = Draft.submitDraft(state, 7);
  assert.deepEqual(plain(state), { text: "", quote: "", edit: "", attachments: [] });
  return state;
}

test("a refused send puts the Draft back with the bad rows marked", () => {
  const frame = { id: 7, error: { code: -32013, message: "file not sendable", data: { paths: ["/b"] } } };
  assert.equal(Daemon.draftSetDue(sentWith("look"), frame), true);
  const state = Daemon.applyFrame(sentWith("look"), frame);
  assert.deepEqual(plain(state), { text: "look", quote: "", edit: "", attachments: ["/a", "/b"] });
  assert.deepEqual(Draft.attachmentRows(state).map((r) => r.bad), [false, true]);
  assert.equal(state.notice, "That file cannot be sent");
});

test("a refused send keeps the text typed since and restores only the Attachments", () => {
  const state = Daemon.applyFrame(Draft.changeDraft(sentWith("look"), { text: "new" }), { id: 7, error: { code: -32013, message: "x", data: { paths: ["/a"] } } });
  assert.deepEqual(plain(state), { text: "new", quote: "", edit: "", attachments: ["/a", "/b"] });
});

test("an accepted send adds its Messages and keeps the Draft empty", () => {
  const sent = [{ id: "m1", chat: chat.jid, text: "look", time: 5 }, { id: "m2", chat: chat.jid, text: "", time: 5 }];
  const frame = { id: 7, result: { sent: sent } };
  assert.equal(Daemon.draftSetDue(sentWith("look"), frame), false);
  const state = Daemon.applyFrame(sentWith("look"), frame);
  assert.deepEqual(JSON.parse(JSON.stringify(state.openChat.messages.map((m) => m.id))), ["in1", "m1", "m2"]);
  assert.deepEqual(plain(state).attachments, []);
});

test("a draft.paste reply gives the open Chat the paths, or an empty list for a text paste", () => {
  const state = opened();
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.pastedAttachment(state, { id: 1, result: { chat: chat.jid, pasted: ["/a/b.pdf", "/c.png"] } }))), ["/a/b.pdf", "/c.png"]);
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.pastedAttachment(state, { id: 1, result: { chat: chat.jid, pasted: [] } }))), []);
  assert.equal(Draft.pastedAttachment(state, { id: 1, result: { chat: "2@s.whatsapp.net", pasted: ["/a/b.pdf"] } }), null);
  assert.equal(Draft.pastedAttachment(state, { id: 1, result: { chat: chat.jid, draft: {} } }), null);
  assert.equal(Daemon.applyFrame(state, { id: 1, result: { chat: chat.jid, pasted: ["/a/b.pdf"] } }), state);
  assert.deepEqual(JSON.parse(Draft.pasteRequest(3, chat.jid)), { jsonrpc: "2.0", id: 3, method: "draft.paste", params: { chat: chat.jid } });
});

test("the paste errors have a notice", () => {
  assert.equal(Daemon.applyFrame(opened(), { id: 1, error: { code: -32012, message: "30 files at most" } }).notice, "30 files at most");
  assert.equal(Daemon.applyFrame(opened(), { id: 1, error: { code: -32013, message: "file not sendable" } }).notice, "That file cannot be sent");
});

test("a location Message has an Open map view with the link, others have none", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.deepEqual(JSON.parse(JSON.stringify(Message.mapView({ text: "Location: Cafe\n" + url }))), { url });
  assert.equal(Message.mapView({ text: "hello" }), null);
  assert.equal(Message.mapView({ text: url, revoked: true }), null);
});


const refusal = { id: 7, error: { code: -32013, message: "x", data: { paths: ["/a"] } } };
const accepted = { id: 7, result: { sent: [] } };

test("no draft.set goes out while a send is pending", () => {
  const state = Draft.changeDraft(sentWith("look"), { text: "n" });
  assert.equal(Daemon.draftSetDue(state), false);
  assert.equal(Daemon.draftSetDue(opened()), true);
});

test("type-ahead then a refusal gives one draft.set with the text and the Attachments", () => {
  const state = Draft.changeDraft(sentWith("look"), { text: "new" });
  assert.equal(Daemon.draftSetDue(state, refusal), true);
  const after = Daemon.applyFrame(state, refusal);
  const set = JSON.parse(Draft.draftSetRequest(1, chat.jid, Draft.draftOf(after)));
  assert.deepEqual(set.params, { chat: chat.jid, text: "new", quote: "", edit: "", attachments: ["/a", "/b"] });
  assert.equal(Daemon.draftSetDue(after), true); // the send is over: keystrokes store again
});

test("type-ahead then success gives one draft.set with the text only", () => {
  const state = Draft.changeDraft(sentWith("look"), { text: "new" });
  assert.equal(Daemon.draftSetDue(state, accepted), true);
  assert.deepEqual(plain(Daemon.applyFrame(state, accepted)), { text: "new", quote: "", edit: "", attachments: [] });
});

test("no type-ahead then success gives no draft.set", () => {
  assert.equal(Daemon.draftSetDue(sentWith("look"), accepted), false);
});

test("a frame that does not answer the send gives no draft.set", () => {
  assert.equal(Daemon.draftSetDue(sentWith("look"), { id: 8, error: { code: -32013, message: "x" } }), false);
});

test("capText keeps the last valid text when the text passes 65536 code points", () => {
  const ok = "a".repeat(65536);
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.capText(ok, ""))), { text: ok, ok: true });
  assert.deepEqual(JSON.parse(JSON.stringify(Draft.capText(ok + "a", "last"))), { text: "last", ok: false });
  // 😀 is one code point and two UTF-16 units: 40000 of them fit.
  const emoji = "😀".repeat(40000);
  assert.equal(Draft.capText(emoji, "").ok, true);
  assert.equal(Draft.capText(emoji + emoji, "").ok, false);
  assert.equal(Draft.textRefused(opened()).notice, Draft.TEXT_TOO_LONG);
});

test("a paste of files and text asks first, and a switch of Chat drops the question", () => {
  const paths = ["/home/me/docs/a.pdf", "/home/me/docs/b.pdf"];
  let state = Draft.pasteAsked(opened(), paths);
  assert.equal(Draft.pasteQuestion(state, "/home/me").text, "Attach 2 files from ~/docs?");
  assert.equal(Draft.pasteQuestion(Draft.pasteAsked(opened(), ["/home/me/Downloads/a.pdf", "/home/me/.ssh/id_ed25519"]), "/home/me").text,
    "Attach 2 files from 2 folders: ~/Downloads, ~/.ssh?");
  assert.equal(Draft.pasteAsked(opened(), [paths[0]]).pasteChoice.paths.length, 1);
  assert.equal(Draft.pasteQuestion(Draft.pasteAsked(opened(), [paths[0]]), "").text, "Attach 1 file from /home/me/docs?");
  assert.equal(Draft.pasteQuestion(Draft.pasteAnswered(state)), null);
  assert.equal(Draft.pasteQuestion(Timeline.openChat(state, { jid: "2@s.whatsapp.net", name: "Bo", kind: "direct" })), null);
});
