const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");
const plain = (value) => JSON.parse(JSON.stringify(value));

// A small copy of the shape of Omarchy's emojis.json.
const EMOJIS = [
  { e: "😀", k: "grinning face smile grinning happy" },
  { e: "😅", k: "hot grinning face with sweat sweat_smile" },
  { e: "❤️", k: "red heart heart love" },
  { e: "💖", k: "sparkling_heart sparkling heart" },
  { e: "👍", k: "approve thumbs up ok +1 thumbsup" },
];

test("parseEmojis reads the list and gives an empty list for bad input", () => {
  assert.equal(State.parseEmojis(JSON.stringify(EMOJIS)).length, 5);
  assert.deepEqual(plain(State.parseEmojis("not json")), []);
  assert.deepEqual(plain(State.parseEmojis("{}")), []);
  assert.deepEqual(plain(State.parseEmojis("")), []);
});

test("an Emoji Shortcode starts after whitespace or at the start, with 2 or more characters", () => {
  assert.deepEqual(plain(State.shortcodeAt(":sm", 3)), { start: 0, query: "sm" });
  assert.deepEqual(plain(State.shortcodeAt("hi :Thumbs", 10)), { start: 3, query: "thumbs" });
  assert.deepEqual(plain(State.shortcodeAt("ok :+1 later", 6)), { start: 3, query: "+1" });
  assert.equal(State.shortcodeAt(":s", 2), null);
  assert.equal(State.shortcodeAt("at 10:30", 8), null);
  assert.equal(State.shortcodeAt("see https://x", 13), null);
  assert.equal(State.shortcodeAt("Note: ok", 8), null);
  assert.equal(State.shortcodeAt(":smile now", 10), null);
});

test("suggestions match anywhere in the keywords, like Omarchy, in list order", () => {
  assert.deepEqual(plain(State.shortcodeSuggestions(EMOJIS, "hea")), [
    { emoji: "❤️", code: "heart" },
    { emoji: "💖", code: "heart" },
  ]);
  assert.deepEqual(plain(State.shortcodeSuggestions(EMOJIS, "art")), [
    { emoji: "❤️", code: "heart" },
    { emoji: "💖", code: "sparkling_heart" },
  ]);
  assert.deepEqual(plain(State.shortcodeSuggestions(EMOJIS, "sweat")), [{ emoji: "😅", code: "sweat" }]);
  assert.deepEqual(plain(State.shortcodeSuggestions(EMOJIS, "_sm")), [{ emoji: "😅", code: "sweat_smile" }]);
  assert.deepEqual(plain(State.shortcodeSuggestions(EMOJIS, "zzz")), []);
  const many = Array.from({ length: 20 }, (_, i) => ({ e: String(i), k: "cat" }));
  assert.equal(State.shortcodeSuggestions(many, "ca").length, 20);
});

test("a pick replaces the Emoji Shortcode and puts the cursor after the emoji", () => {
  assert.deepEqual(plain(State.insertEmoji("hi :thu there", 3, 7, "👍")), { text: "hi 👍 there", cursor: 5 });
});

test("a closing colon on an exact keyword word replaces it at once", () => {
  assert.deepEqual(plain(State.closeShortcode("hi :thumbsup", 12, EMOJIS)), { text: "hi 👍", cursor: 5 });
  assert.deepEqual(plain(State.closeShortcode(":heart", 6, EMOJIS)), { text: "❤️", cursor: 2 });
  assert.equal(State.closeShortcode(":thumb", 6, EMOJIS), null);
  assert.equal(State.closeShortcode("10:30", 5, EMOJIS), null);
});

test("while the row shows, Enter and Tab pick and the arrows move", () => {
  assert.equal(State.shortcodeKeyAction(State.KEY_RETURN, false), "pick");
  assert.equal(State.shortcodeKeyAction(State.KEY_ENTER, false), "pick");
  assert.equal(State.shortcodeKeyAction(State.KEY_RETURN, true), "default");
  assert.equal(State.shortcodeKeyAction(State.KEY_TAB, false), "pick");
  assert.equal(State.shortcodeKeyAction(State.KEY_LEFT, false), "previous");
  assert.equal(State.shortcodeKeyAction(State.KEY_RIGHT, false), "next");
  assert.equal(State.shortcodeKeyAction(0x41, false), "default");
});
