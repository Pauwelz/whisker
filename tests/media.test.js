const test = require("node:test");
const assert = require("node:assert/strict");
const load = require("./load");

const State = load("WhiskerState.js");
// The vm context has its own prototypes: a round trip makes values comparable.
const plain = (value) => JSON.parse(JSON.stringify(value));

// Media. The golden fixtures cover the frames, these tests cover the decisions of the views.
const photo = { id: "m1", chat: "1@s.whatsapp.net", media: { kind: "image", mime: "image/jpeg", name: "", size: 2048, state: "ready" } };
const pdf = { id: "m2", chat: "1@s.whatsapp.net", media: { kind: "document", mime: "application/pdf", name: "a.pdf", size: 3 * 1024 * 1024, state: "none" } };

test("only a ready sticker shows inside the pop-out; an image shows as a thumbnail in its chip", () => {
  assert.equal(State.showsInline(photo), false);
  assert.equal(State.showsInline({ ...photo, media: { ...photo.media, kind: "sticker" } }), true);
  assert.equal(State.showsInline({ ...photo, media: { ...photo.media, kind: "sticker", state: "downloading" } }), false);
  assert.equal(State.showsInline({ ...photo, media: { ...photo.media, kind: "video" } }), false);
  assert.equal(State.showsInline({ id: "t", chat: "c" }), false);
});

test("the Plugin asks for each image path once, and a media.open result stores it", () => {
  let state = State.openChat(State.initialState(), { jid: photo.chat, name: "Ann", kind: "direct" });
  state = State.applyFrame(state, { method: "messageChanged", params: { message: { ...photo, time: 1, text: "" } } });
  state = State.applyFrame(state, { method: "messageChanged", params: { message: { ...photo, id: "m3", time: 2, text: "", media: { ...photo.media, thumbnail: "x" } } } });
  assert.deepEqual(plain(State.mediaToLoad(state).map((m) => m.id)), ["m1"]); // m3 has WhatsApp's thumbnail
  state = State.markMediaRequested(state, State.mediaToLoad(state));
  assert.deepEqual(plain(State.mediaToLoad(state)), []);
  assert.equal(State.mediaPath(state, photo), "");
  state = State.applyFrame(state, { id: 1, result: { chat: photo.chat, id: "m1", path: "/run/user/1000/whisker/media/x/m1.jpg" } });
  assert.equal(State.mediaPath(state, photo), "/run/user/1000/whisker/media/x/m1.jpg");
  assert.equal(State.mediaPath(State.closeChat(state), photo), "");
});

test("Media rows say what they are and what the user can do", () => {
  assert.equal(State.mediaLabel(pdf), "Document: a.pdf (3.0 MB)");
  assert.equal(State.mediaLabel(photo), "Image (2.0 KB)");
  assert.equal(State.mediaLabel({ ...photo, media: { ...photo.media, kind: "voice", duration: 75 } }), "Voice note (1:15, 2.0 KB)");
  assert.equal(State.mediaLabel({ ...photo, media: { ...photo.media, kind: "voice", duration: 9 } }), "Voice note (0:09, 2.0 KB)");
  assert.equal(State.canDownloadMedia(pdf), true);
  assert.equal(State.canDownloadMedia({ ...pdf, media: { ...pdf.media, state: "failed" } }), true);
  assert.equal(State.canDownloadMedia(photo), false);
  assert.equal(State.canOpenMedia(photo), true);
  assert.equal(State.mediaStatus({ ...pdf, media: { ...pdf.media, state: "downloading" } }), "Downloading…");
  assert.equal(State.mediaStatus({ ...pdf, media: { ...pdf.media, state: "failed" } }), "Download failed");
});

test("video opens in mpv and everything else in the default app", () => {
  assert.deepEqual(plain(State.openCommand({ media: { kind: "video" } }, "/p/v.mp4")), ["mpv", "/p/v.mp4"]);
  assert.deepEqual(plain(State.openCommand({ media: { kind: "video", gif: true } }, "/p/g.mp4")), ["mpv", "--loop", "/p/g.mp4"]);
  assert.equal(State.mediaLabel({ media: { kind: "video", gif: true, size: 2048 } }), "GIF (2.0 KB)");
  assert.deepEqual(plain(State.openCommand({ media: { kind: "document" } }, "/p/a.pdf")), ["xdg-open", "/p/a.pdf"]);
  assert.deepEqual(plain(State.openCommand({ media: { kind: "voice" } }, "/p/v.ogg")), ["mpv", "--no-video", "/p/v.ogg"]);
});

test("Media requests carry the Chat and the Message id", () => {
  assert.deepEqual(JSON.parse(State.mediaDownloadRequest(1, pdf)), { jsonrpc: "2.0", id: 1, method: "media.download", params: { chat: pdf.chat, id: "m2" } });
  assert.equal(JSON.parse(State.mediaOpenRequest(2, pdf)).method, "media.open");
  assert.deepEqual(JSON.parse(State.mediaSaveRequest(3, pdf, "/home/u/a.pdf")).params, { chat: pdf.chat, id: "m2", path: "/home/u/a.pdf" });
  assert.deepEqual(JSON.parse(State.mediaReleaseRequest(4, pdf.chat)).params, { chat: pdf.chat });
});

test("a voice note shows Play or Stop, and playing sends media.played", () => {
  const voice = { id: "v1", chat: "1@s.whatsapp.net", media: { kind: "voice", state: "ready" } };
  const key = State.mediaKey(voice.chat, voice.id);
  assert.equal(State.openLabel(voice, ""), "Play");
  assert.equal(State.openLabel(voice, key), "Stop");
  assert.equal(State.openLabel(voice, "other|id"), "Play");
  assert.equal(State.openLabel(pdf, ""), "Open");
  assert.equal(State.isPlaying("", voice), false);
  assert.deepEqual(plain(JSON.parse(State.playedRequest(3, voice))), { jsonrpc: "2.0", id: 3, method: "media.played", params: { chat: voice.chat, id: "v1" } });
});

test("Media is downloading only in that state", () => {
  assert.equal(State.isDownloading({ ...pdf, media: { ...pdf.media, state: "downloading" } }), true);
  assert.equal(State.isDownloading(pdf), false);
  assert.equal(State.isDownloading({ id: "t", chat: "c" }), false);
});
