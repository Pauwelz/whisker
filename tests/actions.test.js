const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");
const plain = (value) => JSON.parse(JSON.stringify(value));
const mine = (extra = {}) => ({ id: "m1", fromMe: true, text: "hi", state: "sent", time: 1000, ...extra });
const theirs = (extra = {}) => ({ id: "m2", fromMe: false, text: "hi", senderName: "Ann", ...extra });
const media = (kind, state) => ({ media: { kind, state } });
const actions = (m) => plain(State.actionsFor(m));

test("a Tombstone only allows delete for me", () => {
  assert.deepEqual(actions(mine({ revoked: true })), ["deleteForMe"]);
});

test("a text Message of others allows reply, react, copy and delete for me", () => {
  assert.deepEqual(actions(theirs()), ["reply", "react", "copy", "deleteForMe"]);
});

test("a text Message of the user also allows edit and delete for everyone", () => {
  assert.deepEqual(actions(mine()), ["reply", "react", "copy", "edit", "deleteForEveryone"]);
});

test("a failed Message of the user allows retry and no edit", () => {
  assert.deepEqual(actions(mine({ state: "failed" })), ["reply", "react", "copy", "retry", "deleteForEveryone"]);
});

test("Media blocks edit and follows the Media state", () => {
  assert.deepEqual(actions(mine({ text: "", ...media("image", "none") })), ["reply", "react", "download", "deleteForEveryone"]);
  assert.deepEqual(actions(theirs({ ...media("document", "failed") })), ["reply", "react", "copy", "download", "deleteForMe"]);
  assert.deepEqual(actions(theirs({ ...media("document", "downloading") })), ["reply", "react", "copy", "deleteForMe"]);
  assert.deepEqual(actions(theirs({ ...media("image", "ready") })), ["reply", "react", "copy", "open", "saveAs", "deleteForMe"]);
});

test("audio and voice play when ready; voice does not open", () => {
  assert.deepEqual(actions(theirs({ text: "", ...media("audio", "ready") })), ["reply", "react", "play", "open", "saveAs", "deleteForMe"]);
  assert.deepEqual(actions(theirs({ text: "", ...media("voice", "ready") })), ["reply", "react", "play", "saveAs", "deleteForMe"]);
});

test("a sticker has no Media actions", () => {
  assert.deepEqual(actions(theirs({ text: "", ...media("sticker", "ready") })), ["reply", "react", "deleteForMe"]);
});

test("the menu drops react and edit when the Message cannot take them", () => {
  assert.deepEqual(plain(State.menuActions(mine({ state: "failed" }), 1001)), ["reply", "copy", "retry", "deleteForEveryone"]);
  assert.deepEqual(plain(State.menuActions(mine(), 1001)), ["reply", "react", "copy", "edit", "deleteForEveryone"]);
  assert.deepEqual(plain(State.menuActions(mine(), 1000 + State.EDIT_WINDOW_SECONDS + 1)), ["reply", "react", "copy", "deleteForEveryone"]);
});

test("the menu offers delete for me, also for a Tombstone", () => {
  assert.deepEqual(plain(State.menuActions(theirs(), 1)), ["reply", "react", "copy", "deleteForMe"]);
  assert.deepEqual(plain(State.menuActions(mine({ revoked: true }), 1)), ["deleteForMe"]);
});

test("every action has a label", () => {
  for (const name of ["reply", "react", "copy", "edit", "retry", "download", "play", "open", "saveAs", "deleteForMe", "deleteForEveryone"]) {
    assert.ok(State.ACTION_LABELS[name], name);
  }
});

test("reaction chips mark the own Reaction and name who reacted", () => {
  const m = { id: "m", reactions: [{ fromMe: false, senderName: "Ann", emoji: "👍" }, { fromMe: true, senderName: "", emoji: "👍" }, { fromMe: false, senderName: "Bob", emoji: "🙏" }] };
  assert.deepEqual(plain(State.reactionChips(m)), [
    { emoji: "👍", count: 2, mine: true, names: "Ann, You" },
    { emoji: "🙏", count: 1, mine: false, names: "Bob" },
  ]);
});
