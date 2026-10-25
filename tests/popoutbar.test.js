const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Daemon = load("state/Daemon.js");

test("the status line hides when whisker is linked and shows for every other screen", () => {
  const linked = Daemon.applyFrame(Daemon.initialState(), { method: "linked" });
  assert.equal(Daemon.statusLineShown(linked), false);
  assert.equal(Daemon.statusLineShown(Daemon.initialState()), true);
  assert.equal(Daemon.statusLineShown({ screen: "linking", message: "Scan the QR code" }), true);
  assert.equal(Daemon.statusLineShown({ screen: "error", message: "Failed" }), true);
  assert.equal(Daemon.statusLineShown({ screen: "keyring-unavailable", message: "No keyring" }), true);
});

test("the header shows the unread count after the name, and nothing at zero", () => {
  assert.equal(Daemon.unreadSuffix({ unreadChats: 3 }), " · 3 unread");
  assert.equal(Daemon.unreadSuffix({ unreadChats: 0 }), "");
  assert.equal(Daemon.unreadSuffix({}), "");
});
