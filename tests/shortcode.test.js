const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Common = load("state/Common.js");
const Shortcode = load("state/Shortcode.js");
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
  assert.equal(Shortcode.parseEmojis(JSON.stringify(EMOJIS)).length, 5);
  assert.deepEqual(plain(Shortcode.parseEmojis("not json")), []);
  assert.deepEqual(plain(Shortcode.parseEmojis("{}")), []);
  assert.deepEqual(plain(Shortcode.parseEmojis("")), []);
});

test("an Emoji Shortcode starts after whitespace or at the start, with 2 or more characters", () => {
  assert.deepEqual(plain(Shortcode.shortcodeAt(":sm", 3)), { start: 0, query: "sm" });
  assert.deepEqual(plain(Shortcode.shortcodeAt("hi :Thumbs", 10)), { start: 3, query: "thumbs" });
  assert.deepEqual(plain(Shortcode.shortcodeAt("ok :+1 later", 6)), { start: 3, query: "+1" });
  assert.equal(Shortcode.shortcodeAt(":s", 2), null);
  assert.equal(Shortcode.shortcodeAt("at 10:30", 8), null);
  assert.equal(Shortcode.shortcodeAt("see https://x", 13), null);
  assert.equal(Shortcode.shortcodeAt("Note: ok", 8), null);
  assert.equal(Shortcode.shortcodeAt(":smile now", 10), null);
});

test("suggestions match anywhere in the keywords, like Omarchy, in list order", () => {
  assert.deepEqual(plain(Shortcode.shortcodeSuggestions(EMOJIS, "hea")), [
    { emoji: "❤️", code: "heart" },
    { emoji: "💖", code: "heart" },
  ]);
  assert.deepEqual(plain(Shortcode.shortcodeSuggestions(EMOJIS, "art")), [
    { emoji: "❤️", code: "heart" },
    { emoji: "💖", code: "sparkling_heart" },
  ]);
  assert.deepEqual(plain(Shortcode.shortcodeSuggestions(EMOJIS, "sweat")), [{ emoji: "😅", code: "sweat" }]);
  assert.deepEqual(plain(Shortcode.shortcodeSuggestions(EMOJIS, "_sm")), [{ emoji: "😅", code: "sweat_smile" }]);
  assert.deepEqual(plain(Shortcode.shortcodeSuggestions(EMOJIS, "zzz")), []);
  const many = Array.from({ length: 20 }, (_, i) => ({ e: String(i), k: "cat" }));
  assert.equal(Shortcode.shortcodeSuggestions(many, "ca").length, 20);
});

test("a pick replaces the Emoji Shortcode and puts the cursor after the emoji", () => {
  assert.deepEqual(plain(Shortcode.insertEmoji("hi :thu there", 3, 7, "👍")), { text: "hi 👍 there", cursor: 5 });
});

test("a closing colon on an exact keyword word replaces it at once", () => {
  assert.deepEqual(plain(Shortcode.closeShortcode("hi :thumbsup", 12, EMOJIS)), { text: "hi 👍", cursor: 5 });
  assert.deepEqual(plain(Shortcode.closeShortcode(":heart", 6, EMOJIS)), { text: "❤️", cursor: 2 });
  assert.equal(Shortcode.closeShortcode(":thumb", 6, EMOJIS), null);
  assert.equal(Shortcode.closeShortcode("10:30", 5, EMOJIS), null);
});

test("while the row shows, Enter and Tab pick and the arrows move", () => {
  assert.equal(Shortcode.shortcodeKeyAction(Common.KEY_RETURN, false), "pick");
  assert.equal(Shortcode.shortcodeKeyAction(Common.KEY_ENTER, false), "pick");
  assert.equal(Shortcode.shortcodeKeyAction(Common.KEY_RETURN, true), "default");
  assert.equal(Shortcode.shortcodeKeyAction(Shortcode.KEY_TAB, false), "pick");
  assert.equal(Shortcode.shortcodeKeyAction(Shortcode.KEY_LEFT, false), "previous");
  assert.equal(Shortcode.shortcodeKeyAction(Shortcode.KEY_RIGHT, false), "next");
  assert.equal(Shortcode.shortcodeKeyAction(0x41, false), "default");
});
