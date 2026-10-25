.pragma library

.import "Common.js" as Common

// The Emoji Shortcode row of the composer: the emoji list, the matches and the keys.

// Qt key codes that the Emoji Shortcode row uses.
var KEY_TAB = 0x01000001
var KEY_LEFT = 0x01000012
var KEY_RIGHT = 0x01000014

// The emoji list of Omarchy's picker ($OMARCHY_PATH/shell/plugins/emojis/emojis.json): a list of {e, k}.
// `k` holds the keyword words, among them the shortcodes. Bad JSON gives an empty list, so Emoji Shortcodes are off.
function parseEmojis(raw) {
  try {
    var data = JSON.parse(String(raw || ""))
    return Array.isArray(data) ? data : []
  } catch (e) {
    return []
  }
}

// The Emoji Shortcode that the user types before `cursor` in `text`, as { start, query }, or null.
// It starts with ":" at the text start or after whitespace, and has 2 or more of a-z 0-9 _ +. So "10:30" and "https://" are none.
function shortcodeAt(text, cursor) {
  var match = /(^|\s):([a-z0-9_+]{2,})$/i.exec(String(text).slice(0, cursor))
  return match ? { start: match.index + match[1].length, query: match[2].toLowerCase() } : null
}

// The emoji whose keywords contain `query`, as Omarchy's picker matches, in Omarchy's order, as { emoji, code }.
// `code` is the keyword word that holds the query: an exact word first, then a word that starts with it, then any.
function shortcodeSuggestions(emojis, query) {
  var out = []
  for (var i = 0; i < emojis.length; i++) {
    var words = String(emojis[i].k || "").toLowerCase().split(" ")
    var code = words.filter(function(w) { return w === query })[0]
      || words.filter(function(w) { return w.indexOf(query) === 0 })[0]
      || words.filter(function(w) { return w.indexOf(query) >= 0 })[0]
    if (emojis[i].e && code) out.push({ emoji: emojis[i].e, code: code })
  }
  return out
}

// The text and cursor after `emoji` replaces the Emoji Shortcode from `start` to `cursor`.
function insertEmoji(text, start, cursor, emoji) {
  return { text: text.slice(0, start) + emoji + text.slice(cursor), cursor: start + emoji.length }
}

// The text and cursor when the user types ":" at `cursor` and so closes a full Emoji Shortcode such as ":thumbsup:", or null.
// The code must be one keyword word. When more emoji share it, the first one wins.
function closeShortcode(text, cursor, emojis) {
  var shortcode = shortcodeAt(text, cursor)
  if (!shortcode) return null
  for (var i = 0; i < emojis.length; i++) {
    if (emojis[i].e && String(emojis[i].k || "").split(" ").indexOf(shortcode.query) >= 0) return insertEmoji(text, shortcode.start, cursor, emojis[i].e)
  }
  return null
}

// What a key does while the Emoji Shortcode row shows: "previous", "next", "pick" or "default".
// Enter picks, so it does not send. Escape is handled apart, because Qt runs Keys.onEscapePressed first.
function shortcodeKeyAction(key, shift) {
  if (key === KEY_LEFT) return "previous"
  if (key === KEY_RIGHT) return "next"
  if (key === KEY_TAB || ((key === Common.KEY_RETURN || key === Common.KEY_ENTER) && !shift)) return "pick"
  return "default"
}
