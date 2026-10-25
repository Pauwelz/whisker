const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");
const plain = (value) => JSON.parse(JSON.stringify(value));

// Local time of a day in October 2026, so the tests do not depend on the time zone.
const at = (day, hour = 12, minute = 0) => new Date(2026, 9, day, hour, minute).getTime() / 1000;
const from = (sender, id, time, extra = {}) => ({ id, sender, fromMe: false, senderName: sender, time, text: id, ...extra });
const mine = (id, time, extra = {}) => ({ id, sender: "", fromMe: true, time, text: id, state: "sent", ...extra });
const shape = (rows) => plain(rows).map((r) => (r.kind === "message" ? (r.continued ? "~" : "") + r.message.id : r.kind));

test("a run from one sender hides time and sender on the next Messages", () => {
  const rows = State.timelineRows([from("a", "m1", at(1)), from("a", "m2", at(1, 12, 1)), from("b", "m3", at(1, 12, 2)), from("a", "m4", at(1, 12, 3))]);
  assert.deepEqual(shape(rows), ["day", "m1", "~m2", "m3", "m4"]);
});

test("the Messages of the user form a run of their own", () => {
  const rows = State.timelineRows([mine("m1", at(1)), mine("m2", at(1)), from("a", "m3", at(1)), mine("m4", at(1))]);
  assert.deepEqual(shape(rows), ["day", "m1", "~m2", "m3", "m4"]);
});

test("a new day starts a run and adds a day separator", () => {
  const rows = State.timelineRows([from("a", "m1", at(1, 23, 59)), from("a", "m2", at(2, 0, 1))]);
  assert.deepEqual(shape(rows), ["day", "m1", "day", "m2"]);
  assert.equal(rows[0].label, "2026-10-01");
  assert.equal(rows[2].label, "2026-10-02");
});

test("a system line ends a run and has no sender", () => {
  const rows = State.timelineRows([from("a", "m1", at(1)), { id: "s1", system: true, time: at(1), text: "Mama added ~ Cleo" }, from("a", "m2", at(1))]);
  assert.deepEqual(shape(rows), ["day", "m1", "system", "m2"]);
});

test("no Messages give no rows", () => {
  assert.deepEqual(plain(State.timelineRows([])), []);
});

test("rowIndexOf finds the row of a Message id, or -1", () => {
  const rows = State.timelineRows([from("a", "m1", at(1)), from("a", "m2", at(1))]);
  assert.equal(State.rowIndexOf(rows, "m2"), 2);
  assert.equal(State.rowIndexOf(rows, "gone"), -1);
});

test("a Participant keeps one colour, and Participants spread over the palette", () => {
  assert.equal(State.senderColor("a@s.whatsapp.net"), State.senderColor("a@s.whatsapp.net"));
  const colours = new Set(["1", "2", "3", "4", "5", "6", "7", "8"].map((n) => State.senderColor(n + "@s.whatsapp.net")));
  assert.ok(colours.size > 3);
  assert.match(State.senderColor("x"), /^#[0-9a-f]{6}$/);
});

test("the sender label: You, the name, or ~ with name and number for a Push Name", () => {
  assert.equal(State.senderLabel(mine("m1", 1)), "You");
  assert.equal(State.senderLabel({ fromMe: false, senderName: "Ann" }), "Ann");
  assert.equal(State.senderLabel({ fromMe: false, senderName: "Cleo", senderPush: true, sender: "31600:5@s.whatsapp.net" }), "~ Cleo +31600");
});

test("the Delivery State column: a mark per state, red for a failed Message", () => {
  const mark = (state) => plain(State.deliveryMark(mine("m", 1, { state })));
  assert.deepEqual(mark("failed"), { text: "!", tone: "error" });
  assert.deepEqual(mark("read"), { text: "✓✓", tone: "read" });
  assert.deepEqual(mark("played"), { text: "✓✓", tone: "read" });
  assert.deepEqual(mark("delivered"), { text: "✓✓", tone: "normal" });
  assert.deepEqual(mark("sent"), { text: "✓", tone: "normal" });
  assert.deepEqual(mark("pending"), { text: "…", tone: "normal" });
  assert.deepEqual(plain(State.deliveryMark(from("a", "m", 1))), { text: "", tone: "normal" });
});

test("a failed Message shows the not sent line; others do not", () => {
  assert.equal(State.failedLine(mine("m", 1, { state: "failed" })), "not sent · retry");
  assert.equal(State.failedLine(mine("m", 1)), "");
});

test("the time label is HH:MM", () => {
  assert.equal(State.timeLabel(at(1, 9, 5)), "09:05");
});

test("a Quote keeps the id of the quoted Message for the jump", () => {
  assert.equal(State.quoteView({ quote: { id: "m9", fromMe: false, senderName: "Ann", text: "x" } }).id, "m9");
});

test("(edited) marks an edited Message, not a Tombstone", () => {
  assert.equal(State.editedLabel(mine("m", 1, { edited: true })), "(edited)");
  assert.equal(State.editedLabel(mine("m", 1, { edited: true, revoked: true })), "");
});

// Applies listEdits to a copy of the keys, as the Panel applies them to its ListModel.
const edited = (before, edits) => {
  const keys = before.slice();
  edits.forEach((e) => (e.key === undefined ? keys.splice(e.remove, 1) : keys.splice(e.insert, 0, e.key)));
  return keys;
};

test("rowKey gives each row of a timeline its own key", () => {
  const rows = State.timelineRows([from("a", "m1", at(1)), { id: "s1", system: true, time: at(1), text: "x" }, from("a", "m2", at(2))]);
  assert.deepEqual(plain(rows.map(State.rowKey)), ["day 2026-10-01", "m m1", "m s1", "day 2026-10-02", "m m2"]);
});

test("listEdits adds older rows at the end and leaves the others in place", () => {
  const edits = plain(State.listEdits(["m3", "m2"], ["m3", "m2", "m1", "day"]));
  assert.deepEqual(edits, [{ insert: 2, key: "m1" }, { insert: 3, key: "day" }]);
});

test("listEdits puts a new row at the front", () => {
  assert.deepEqual(plain(State.listEdits(["m2", "m1"], ["m3", "m2", "m1"])), [{ insert: 0, key: "m3" }]);
});

test("listEdits removes from the end first, so each index is still right", () => {
  const before = ["m4", "m3", "m2", "m1"];
  const edits = State.listEdits(before, ["m4", "m2"]);
  assert.deepEqual(plain(edits), [{ remove: 3 }, { remove: 1 }]);
  assert.deepEqual(edited(before, edits), ["m4", "m2"]);
});

test("listEdits mixes removes and inserts into the new order", () => {
  const before = ["m5", "m4", "day b", "m3", "m2"];
  const after = ["m6", "m5", "day b", "m3", "m2", "m1", "day a"];
  assert.deepEqual(edited(before, State.listEdits(before, after)), after);
});

test("listEdits gives null when kept rows change order", () => {
  assert.equal(State.listEdits(["m1", "m2"], ["m2", "m1"]), null);
});
