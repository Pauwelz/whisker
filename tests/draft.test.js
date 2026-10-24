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
  assert.deepEqual(JSON.parse(JSON.stringify(State.draftOf(state))), { text: "yes", quote: "in1", edit: "", attachment: "" });
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
  assert.deepEqual(JSON.parse(State.draftSetRequest(5, chat.jid, { text: "a", quote: "q", edit: "", attachment: "/a/b.png" })).params, { chat: chat.jid, text: "a", quote: "q", edit: "", attachment: "/a/b.png" });
  assert.equal(JSON.parse(State.draftGetRequest(6, chat.jid)).method, "draft.get");
});

test("a staged file is part of the Draft and keeps the text", () => {
  let state = State.changeDraft(opened(), { text: "look" });
  state = State.changeDraft(state, { attachment: "/home/me/photo.png" });
  assert.equal(State.attachmentName(State.draftOf(state)), "photo.png");
  state = State.changeDraft(state, { attachment: "" });
  assert.deepEqual(JSON.parse(JSON.stringify(State.draftOf(state))), { text: "look", quote: "", edit: "", attachment: "" });
});

test("starting an Edit drops the staged file", () => {
  let state = State.changeDraft(opened(), { attachment: "/a/b.png" });
  state = State.changeDraft(state, { edit: "in1", text: "lunch?" });
  assert.equal(State.draftOf(state).attachment, "");
  assert.equal(State.draftOf(state).edit, "in1");
});

test("staging a file during an Edit is refused", () => {
  const state = State.changeDraft(opened(), { edit: "in1", text: "lunch?" });
  assert.equal(State.changeDraft(state, { attachment: "/a/b.png" }), state);
});

test("a staged file sends as Media with the text as caption, and without the Quote", () => {
  const sent = JSON.parse(State.composerRequest(1, chat.jid, "", "look", "in1", "/a/b.png"));
  assert.equal(sent.method, "chat.sendMedia");
  assert.deepEqual(sent.params, { chat: chat.jid, path: "/a/b.png", caption: "look" });
  assert.equal(JSON.parse(State.composerRequest(2, chat.jid, "", "hi", "", "")).method, "chat.send");
});

test("clearing the Draft clears the staged file", () => {
  const state = State.clearDraft(State.changeDraft(opened(), { attachment: "/a/b.png", text: "x" }));
  assert.equal(State.draftOf(state).attachment, "");
});
