const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");
const chat = { jid: "1@s.whatsapp.net", name: "Ann", kind: "direct" };
const message = { id: "in1", chat: chat.jid, text: "lunch?", time: 1 };

function opened() {
  return State.upsertMessage(State.openChat(State.initialState(), chat), message);
}

test("changing the Draft keeps the pending Quote and Edit target", () => {
  let state = State.changeDraft(opened(), { quote: "in1" });
  state = State.changeDraft(state, { text: "yes" });
  assert.deepEqual(JSON.parse(JSON.stringify(State.draftOf(state))), { text: "yes", quote: "in1", edit: "", attachments: [] });
  assert.equal(State.draftTarget(state, "quote").id, "in1");
  assert.equal(State.draftTarget(state, "edit"), null);
});

test("a change that changes nothing returns the same state", () => {
  const state = opened();
  assert.equal(State.changeDraft(state, { text: "" }), state);
});

test("a loaded Draft does not overwrite text that the user already typed", () => {
  let state = State.changeDraft(opened(), { text: "typed" });
  state = State.applyFrame(state, { id: 1, result: { chat: chat.jid, draft: { text: "old", quote: "", edit: "" } } });
  assert.equal(State.draftOf(state).text, "typed");
});

test("a Draft of another Chat is dropped", () => {
  const state = State.applyFrame(opened(), { id: 1, result: { chat: "2@s.whatsapp.net", draft: { text: "x", quote: "", edit: "" } } });
  assert.equal(State.draftOf(state).text, "");
});

test("the Draft requests carry the Chat and the Draft", () => {
  assert.deepEqual(JSON.parse(State.draftSetRequest(5, chat.jid, { text: "a", quote: "q", edit: "", attachments: ["/a/b.png", "/c.pdf"] })).params, { chat: chat.jid, text: "a", quote: "q", edit: "", attachments: ["/a/b.png", "/c.pdf"] });
  assert.equal(JSON.parse(State.draftGetRequest(6, chat.jid)).method, "draft.get");
});

function plain(state) {
  return JSON.parse(JSON.stringify(State.draftOf(state)));
}

test("Attachments are part of the Draft, in order, and keep the text", () => {
  let state = State.changeDraft(opened(), { text: "look" });
  state = State.changeDraft(state, { attachments: ["/home/me/photo.png", "/home/me/b.pdf"] });
  assert.deepEqual(State.attachmentRows(state).map((r) => r.label), ["Attached: photo.png", "Attached: b.pdf"]);
  state = State.changeDraft(state, { attachments: [] });
  assert.deepEqual(plain(state), { text: "look", quote: "", edit: "", attachments: [] });
});

test("a paste adds to the end and skips a path that is already there", () => {
  const draft = { attachments: ["/a", "/b"] };
  assert.deepEqual(JSON.parse(JSON.stringify(State.appendAttachments(draft, ["/b", "/c", "/c"]))), ["/a", "/b", "/c"]);
});

test("removing one row keeps the other Attachments", () => {
  let state = State.changeDraft(opened(), { attachments: ["/a", "/b", "/c"] });
  state = State.changeDraft(state, { attachments: ["/a", "/c"] });
  assert.deepEqual(plain(state).attachments, ["/a", "/c"]);
});

test("starting an Edit drops the Attachments", () => {
  let state = State.changeDraft(opened(), { attachments: ["/a/b.png"] });
  state = State.changeDraft(state, { edit: "in1", text: "lunch?" });
  assert.deepEqual(plain(state).attachments, []);
  assert.equal(State.draftOf(state).edit, "in1");
});

test("adding Attachments during an Edit is refused", () => {
  const state = State.changeDraft(opened(), { edit: "in1", text: "lunch?" });
  assert.equal(State.changeDraft(state, { attachments: ["/a/b.png"] }), state);
});

test("adding Attachments drops the Quote, and starting a Quote drops the files", () => {
  let state = State.changeDraft(opened(), { quote: "in1", text: "x" });
  state = State.changeDraft(state, { attachments: ["/a/b.png"] });
  assert.deepEqual(plain(state), { text: "x", quote: "", edit: "", attachments: ["/a/b.png"] });
  state = State.changeDraft(state, { quote: "in1" });
  assert.deepEqual(plain(state), { text: "x", quote: "in1", edit: "", attachments: [] });
});

test("Attachments send as Media with the text as caption", () => {
  const sent = JSON.parse(State.composerRequest(1, chat.jid, "", "look", "", ["/a/b.png", "/c.pdf"]));
  assert.equal(sent.method, "chat.sendMedia");
  assert.deepEqual(sent.params, { chat: chat.jid, paths: ["/a/b.png", "/c.pdf"], caption: "look" });
  assert.equal(JSON.parse(State.composerRequest(2, chat.jid, "", "hi", "", [])).method, "chat.send");
});

test("clearing the Draft clears the Attachments", () => {
  const state = State.clearDraft(State.changeDraft(opened(), { attachments: ["/a/b.png"], text: "x" }));
  assert.deepEqual(plain(state).attachments, []);
});

test("a paste can attach files except during an Edit, and the rows show only with files", () => {
  assert.equal(State.canAttach(""), true);
  assert.equal(State.canAttach("in1"), false);
  assert.equal(State.hasAttachment({ attachments: ["/a/b.png"] }), true);
  assert.equal(State.hasAttachment(State.draftOf(opened())), false);
});

test("a loaded Draft marks the paths that the Daemon lists as unsendable", () => {
  const state = State.applyFrame(opened(), { id: 1, result: { chat: chat.jid, draft: { text: "", quote: "", edit: "", attachments: ["/a", "/b"] }, unsendable: ["/b"] } });
  assert.deepEqual(State.attachmentRows(state).map((r) => [r.label, r.bad]), [["Attached: a", false], ["Not sendable: b", true]]);
});

test("removing a marked row removes its mark", () => {
  let state = State.applyFrame(opened(), { id: 1, result: { chat: chat.jid, draft: { text: "", quote: "", edit: "", attachments: ["/a", "/b"] }, unsendable: ["/b"] } });
  state = State.changeDraft(state, { attachments: ["/a"] });
  state = State.changeDraft(state, { attachments: ["/a", "/b"] });
  assert.deepEqual(State.attachmentRows(state).map((r) => r.bad), [false, false]);
});

function sentWith(text) {
  let state = State.changeDraft(opened(), { text: text, attachments: ["/a", "/b"] });
  state = State.submitDraft(state, 7);
  assert.deepEqual(plain(state), { text: "", quote: "", edit: "", attachments: [] });
  return state;
}

test("a refused send puts the Draft back with the bad rows marked", () => {
  const frame = { id: 7, error: { code: -32013, message: "file not sendable", data: { paths: ["/b"] } } };
  assert.equal(State.draftSetDue(sentWith("look"), frame), true);
  const state = State.applyFrame(sentWith("look"), frame);
  assert.deepEqual(plain(state), { text: "look", quote: "", edit: "", attachments: ["/a", "/b"] });
  assert.deepEqual(State.attachmentRows(state).map((r) => r.bad), [false, true]);
  assert.equal(state.notice, "That file cannot be sent");
});

test("a refused send keeps the text typed since and restores only the Attachments", () => {
  const state = State.applyFrame(State.changeDraft(sentWith("look"), { text: "new" }), { id: 7, error: { code: -32013, message: "x", data: { paths: ["/a"] } } });
  assert.deepEqual(plain(state), { text: "new", quote: "", edit: "", attachments: ["/a", "/b"] });
});

test("an accepted send adds its Messages and keeps the Draft empty", () => {
  const sent = [{ id: "m1", chat: chat.jid, text: "look", time: 5 }, { id: "m2", chat: chat.jid, text: "", time: 5 }];
  const frame = { id: 7, result: { sent: sent } };
  assert.equal(State.draftSetDue(sentWith("look"), frame), false);
  const state = State.applyFrame(sentWith("look"), frame);
  assert.deepEqual(JSON.parse(JSON.stringify(state.openChat.messages.map((m) => m.id))), ["in1", "m1", "m2"]);
  assert.deepEqual(plain(state).attachments, []);
});

test("a draft.paste reply gives the open Chat the paths, or an empty list for a text paste", () => {
  const state = opened();
  assert.deepEqual(JSON.parse(JSON.stringify(State.pastedAttachment(state, { id: 1, result: { chat: chat.jid, pasted: ["/a/b.pdf", "/c.png"] } }))), ["/a/b.pdf", "/c.png"]);
  assert.deepEqual(JSON.parse(JSON.stringify(State.pastedAttachment(state, { id: 1, result: { chat: chat.jid, pasted: [] } }))), []);
  assert.equal(State.pastedAttachment(state, { id: 1, result: { chat: "2@s.whatsapp.net", pasted: ["/a/b.pdf"] } }), null);
  assert.equal(State.pastedAttachment(state, { id: 1, result: { chat: chat.jid, draft: {} } }), null);
  assert.equal(State.applyFrame(state, { id: 1, result: { chat: chat.jid, pasted: ["/a/b.pdf"] } }), state);
  assert.deepEqual(JSON.parse(State.pasteRequest(3, chat.jid)), { jsonrpc: "2.0", id: 3, method: "draft.paste", params: { chat: chat.jid } });
});

test("the paste errors have a notice", () => {
  assert.equal(State.applyFrame(opened(), { id: 1, error: { code: -32012, message: "30 files at most" } }).notice, "30 files at most");
  assert.equal(State.applyFrame(opened(), { id: 1, error: { code: -32013, message: "file not sendable" } }).notice, "That file cannot be sent");
});

test("a location Message has an Open map view with the link, others have none", () => {
  const url = "https://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000";
  assert.deepEqual(JSON.parse(JSON.stringify(State.mapView({ text: "Location: Cafe\n" + url }))), { url });
  assert.equal(State.mapView({ text: "hello" }), null);
  assert.equal(State.mapView({ text: url, revoked: true }), null);
});


const refusal = { id: 7, error: { code: -32013, message: "x", data: { paths: ["/a"] } } };
const accepted = { id: 7, result: { sent: [] } };

test("no draft.set goes out while a send is pending", () => {
  const state = State.changeDraft(sentWith("look"), { text: "n" });
  assert.equal(State.draftSetDue(state), false);
  assert.equal(State.draftSetDue(opened()), true);
});

test("type-ahead then a refusal gives one draft.set with the text and the Attachments", () => {
  const state = State.changeDraft(sentWith("look"), { text: "new" });
  assert.equal(State.draftSetDue(state, refusal), true);
  const after = State.applyFrame(state, refusal);
  const set = JSON.parse(State.draftSetRequest(1, chat.jid, State.draftOf(after)));
  assert.deepEqual(set.params, { chat: chat.jid, text: "new", quote: "", edit: "", attachments: ["/a", "/b"] });
  assert.equal(State.draftSetDue(after), true); // the send is over: keystrokes store again
});

test("type-ahead then success gives one draft.set with the text only", () => {
  const state = State.changeDraft(sentWith("look"), { text: "new" });
  assert.equal(State.draftSetDue(state, accepted), true);
  assert.deepEqual(plain(State.applyFrame(state, accepted)), { text: "new", quote: "", edit: "", attachments: [] });
});

test("no type-ahead then success gives no draft.set", () => {
  assert.equal(State.draftSetDue(sentWith("look"), accepted), false);
});

test("a frame that does not answer the send gives no draft.set", () => {
  assert.equal(State.draftSetDue(sentWith("look"), { id: 8, error: { code: -32013, message: "x" } }), false);
});
