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
  assert.deepEqual(JSON.parse(JSON.stringify(State.draftOf(state))), { text: "yes", quote: "in1", edit: "" });
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
  assert.deepEqual(JSON.parse(State.draftSetRequest(5, chat.jid, { text: "a", quote: "q", edit: "" })).params, { chat: chat.jid, text: "a", quote: "q", edit: "" });
  assert.equal(JSON.parse(State.draftGetRequest(6, chat.jid)).method, "draft.get");
});
