const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");

test("the status line hides when whisker is linked and shows for every other screen", () => {
  const linked = State.applyFrame(State.initialState(), { method: "linked" });
  assert.equal(State.statusLineShown(linked), false);
  assert.equal(State.statusLineShown(State.initialState()), true);
  assert.equal(State.statusLineShown({ screen: "linking", message: "Scan the QR code" }), true);
  assert.equal(State.statusLineShown({ screen: "error", message: "Failed" }), true);
  assert.equal(State.statusLineShown({ screen: "keyring-unavailable", message: "No keyring" }), true);
});

test("the header shows the unread count after the name, and nothing at zero", () => {
  assert.equal(State.unreadSuffix({ unreadChats: 3 }), " · 3 unread");
  assert.equal(State.unreadSuffix({ unreadChats: 0 }), "");
  assert.equal(State.unreadSuffix({}), "");
});
