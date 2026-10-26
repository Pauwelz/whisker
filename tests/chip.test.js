const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Media = load("state/Media.js");
const plain = (value) => JSON.parse(JSON.stringify(value));

const media = (kind, state, extra) => ({ id: "m", chat: "c", fromMe: false, text: "", state: "", media: { kind, mime: "", name: "", size: 10, state, ...extra } });

test("the thumbnail is a data URL, else the downloaded image file, else empty", () => {
  assert.equal(Media.thumbnailSource(media("video", "none", { thumbnail: "/9j/4A==" }), ""), "data:image/jpeg;base64,/9j/4A==");
  assert.equal(Media.thumbnailSource(media("image", "ready", { thumbnail: "/9j/4A==" }), "/run/m.jpg"), "data:image/jpeg;base64,/9j/4A==");
  assert.equal(Media.thumbnailSource(media("image", "ready"), "/run/m.jpg"), "file:///run/m.jpg");
  assert.equal(Media.thumbnailSource(media("image", "none"), ""), "");
  assert.equal(Media.thumbnailSource(media("video", "ready"), "/run/m.mp4"), ""); // a video file is no image
  assert.equal(Media.thumbnailSource({ text: "hi" }, ""), "");
});

test("a video that is not downloaded is blurred", () => {
  assert.equal(Media.chipBlurred(media("video", "none", { thumbnail: "x" })), true);
  assert.equal(Media.chipBlurred(media("video", "downloading", { thumbnail: "x" })), true);
  assert.equal(Media.chipBlurred(media("video", "ready", { thumbnail: "x" })), false);
  assert.equal(Media.chipBlurred(media("image", "none", { thumbnail: "x" })), false);
});

test("chips without a thumbnail show a file-type badge", () => {
  assert.equal(Media.fileBadge(media("document", "none", { name: "report.pdf" })), "PDF");
  assert.equal(Media.fileBadge(media("document", "none", { name: "a.tar.gz" })), "GZ");
  assert.equal(Media.fileBadge(media("document", "none", { name: "invoice.desktop", mime: "application/pdf" })), "DESKTOP");
  assert.equal(Media.fileBadge(media("document", "none", { name: "a.longext1", mime: "application/pdf" })), "LONGEXT1");
  assert.equal(Media.fileBadge(media("document", "none", { name: "README", mime: "application/pdf" })), "PDF");
  assert.equal(Media.fileBadge(media("document", "none", { mime: "application/pdf" })), "PDF");
  assert.equal(Media.fileBadge(media("audio", "none", { mime: "audio/mpeg" })), "MP3");
  assert.equal(Media.fileBadge(media("audio", "none", { mime: "audio/x-weird" })), "AUDIO");
  assert.equal(Media.fileBadge(media("document", "none")), "FILE");
  assert.equal(Media.fileBadge(media("document", "none", { name: "a.pdf", thumbnail: "x" })), "");
  assert.equal(Media.fileBadge(media("image", "none")), "IMG");
  assert.equal(Media.fileBadge(media("video", "none")), "VID");
  assert.equal(Media.fileBadge(media("video", "none", { gif: true })), "GIF");
  assert.equal(Media.fileBadge(media("image", "none", { thumbnail: "x" })), "");
});

test("a click on the thumbnail or name runs the main action", () => {
  assert.equal(Media.chipMainAction(media("document", "none")), "download");
  assert.equal(Media.chipMainAction(media("document", "failed")), "download");
  assert.equal(Media.chipMainAction(media("document", "downloading")), "");
  assert.equal(Media.chipMainAction(media("document", "ready")), "open");
  assert.equal(Media.chipMainAction(media("audio", "ready")), "play");
  assert.equal(Media.chipMainAction(media("voice", "ready")), "play");
  assert.equal(Media.chipMainAction(media("sticker", "ready")), "");
});

test("the chip links are download, open and save as, and play for audio only", () => {
  assert.deepEqual(plain(Media.chipLinks(media("document", "none"))), [{ action: "download", label: "download" }]);
  assert.deepEqual(plain(Media.chipLinks(media("document", "failed"))), [{ action: "download", label: "retry" }]);
  assert.deepEqual(plain(Media.chipLinks(media("document", "ready"))), [{ action: "open", label: "open" }, { action: "saveAs", label: "save as…" }]);
  assert.deepEqual(plain(Media.chipLinks(media("audio", "ready")).map((l) => l.action)), ["play", "open", "saveAs"]);
  assert.deepEqual(plain(Media.chipLinks(media("voice", "ready")).map((l) => l.action)), ["saveAs"]);
  assert.deepEqual(plain(Media.chipLinks(media("document", "downloading"))), []);
});

test("a received voice note shows a dot until it played", () => {
  const voice = media("voice", "ready");
  assert.equal(Media.unplayedDot(voice), true);
  assert.equal(Media.unplayedDot(media("voice", "ready", { played: true })), false);
  assert.equal(Media.unplayedDot({ ...voice, fromMe: true }), false);
  assert.equal(Media.unplayedDot(media("voice", "none")), false);
  assert.equal(Media.unplayedDot(media("audio", "ready")), false);
});

test("a sticker shows as itself", () => {
  assert.equal(Media.chipShown(media("sticker", "ready")), false);
  assert.equal(Media.chipShown(media("sticker", "none")), true);
  assert.equal(Media.chipShown(media("image", "ready")), true);
  assert.equal(Media.chipShown({ text: "hi" }), false);
});
