const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const Daemon = load("state/Daemon.js");
const Media = load("state/Media.js");
const Timeline = load("state/Timeline.js");
// The vm context has its own prototypes: a round trip makes values comparable.
const plain = (value) => JSON.parse(JSON.stringify(value));

// Media. The golden fixtures cover the frames, these tests cover the decisions of the views.
const photo = { id: "m1", chat: "1@s.whatsapp.net", media: { kind: "image", mime: "image/jpeg", name: "", size: 2048, state: "ready" } };
const pdf = { id: "m2", chat: "1@s.whatsapp.net", media: { kind: "document", mime: "application/pdf", name: "a.pdf", size: 3 * 1024 * 1024, state: "none" } };

test("only a ready sticker shows inside the pop-out; an image shows as a thumbnail in its chip", () => {
  assert.equal(Media.showsInline(photo), false);
  assert.equal(Media.showsInline({ ...photo, media: { ...photo.media, kind: "sticker" } }), true);
  assert.equal(Media.showsInline({ ...photo, media: { ...photo.media, kind: "sticker", state: "downloading" } }), false);
  assert.equal(Media.showsInline({ ...photo, media: { ...photo.media, kind: "video" } }), false);
  assert.equal(Media.showsInline({ id: "t", chat: "c" }), false);
});

test("the Plugin asks for each image path once, and a media.open result stores it", () => {
  let state = Timeline.openChat(Daemon.initialState(), { jid: photo.chat, name: "Ann", kind: "direct" });
  state = Daemon.applyFrame(state, { method: "messageChanged", params: { message: { ...photo, time: 1, text: "" } } });
  state = Daemon.applyFrame(state, { method: "messageChanged", params: { message: { ...photo, id: "m3", time: 2, text: "", media: { ...photo.media, thumbnail: "x" } } } });
  assert.deepEqual(plain(Media.mediaToLoad(state).map((m) => m.id)), ["m1"]); // m3 has WhatsApp's thumbnail
  state = Media.markMediaRequested(state, Media.mediaToLoad(state));
  assert.deepEqual(plain(Media.mediaToLoad(state)), []);
  assert.equal(Media.mediaPath(state, photo), "");
  state = Daemon.applyFrame(state, { id: 1, result: { chat: photo.chat, id: "m1", path: "/run/user/1000/whisker/media/x/m1.jpg" } });
  assert.equal(Media.mediaPath(state, photo), "/run/user/1000/whisker/media/x/m1.jpg");
  assert.equal(Media.mediaPath(Timeline.closeChat(state), photo), "");
});

test("Media rows say what they are and what the user can do", () => {
  assert.equal(Media.mediaLabel(pdf), "Document: a.pdf (3.0 MB)");
  assert.equal(Media.mediaLabel(photo), "Image (2.0 KB)");
  assert.equal(Media.mediaLabel({ ...photo, media: { ...photo.media, kind: "voice", duration: 75 } }), "Voice note (1:15, 2.0 KB)");
  assert.equal(Media.mediaLabel({ ...photo, media: { ...photo.media, kind: "voice", duration: 9 } }), "Voice note (0:09, 2.0 KB)");
  assert.equal(Media.canDownloadMedia(pdf), true);
  assert.equal(Media.canDownloadMedia({ ...pdf, media: { ...pdf.media, state: "failed" } }), true);
  assert.equal(Media.canDownloadMedia(photo), false);
  assert.equal(Media.canOpenMedia(photo), true);
  assert.equal(Media.mediaStatus({ ...pdf, media: { ...pdf.media, state: "downloading" } }), "Downloading…");
  assert.equal(Media.mediaStatus({ ...pdf, media: { ...pdf.media, state: "failed" } }), "Download failed");
});

test("video opens in mpv and everything else in the default app", () => {
  assert.deepEqual(plain(Media.openCommand({ media: { kind: "video" } }, "/p/v.mp4")), ["mpv", "/p/v.mp4"]);
  assert.deepEqual(plain(Media.openCommand({ media: { kind: "video", gif: true } }, "/p/g.mp4")), ["mpv", "--loop", "/p/g.mp4"]);
  assert.equal(Media.mediaLabel({ media: { kind: "video", gif: true, size: 2048 } }), "GIF (2.0 KB)");
  assert.deepEqual(plain(Media.openCommand({ media: { kind: "document" } }, "/p/a.pdf")), ["xdg-open", "/p/a.pdf"]);
  assert.deepEqual(plain(Media.openCommand({ media: { kind: "voice" } }, "/p/v.ogg")), ["mpv", "--no-video", "/p/v.ogg"]);
});

test("Media requests carry the Chat and the Message id", () => {
  assert.deepEqual(JSON.parse(Media.mediaDownloadRequest(1, pdf)), { jsonrpc: "2.0", id: 1, method: "media.download", params: { chat: pdf.chat, id: "m2" } });
  assert.equal(JSON.parse(Media.mediaOpenRequest(2, pdf)).method, "media.open");
  assert.deepEqual(JSON.parse(Media.mediaSaveRequest(3, pdf, "/home/u/a.pdf")).params, { chat: pdf.chat, id: "m2", path: "/home/u/a.pdf" });
  assert.deepEqual(JSON.parse(Media.mediaReleaseRequest(4, pdf.chat)).params, { chat: pdf.chat });
});

test("a voice note shows Play or Stop, and playing sends media.played", () => {
  const voice = { id: "v1", chat: "1@s.whatsapp.net", media: { kind: "voice", state: "ready" } };
  const key = Media.mediaKey(voice.chat, voice.id);
  assert.equal(Media.openLabel(voice, ""), "Play");
  assert.equal(Media.openLabel(voice, key), "Stop");
  assert.equal(Media.openLabel(voice, "other|id"), "Play");
  assert.equal(Media.openLabel(pdf, ""), "Open");
  assert.equal(Media.isPlaying("", voice), false);
  assert.deepEqual(plain(JSON.parse(Media.playedRequest(3, voice))), { jsonrpc: "2.0", id: 3, method: "media.played", params: { chat: voice.chat, id: "v1" } });
});

test("Media is downloading only in that state", () => {
  assert.equal(Media.isDownloading({ ...pdf, media: { ...pdf.media, state: "downloading" } }), true);
  assert.equal(Media.isDownloading(pdf), false);
  assert.equal(Media.isDownloading({ id: "t", chat: "c" }), false);
});
