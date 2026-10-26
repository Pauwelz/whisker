.pragma library

.import "Common.js" as Common
.import "Message.js" as Message

// Media: paths, download and open steps, labels and the Media chip.

// The states of Media. The Daemon sends them (docs/contract.md).
var MEDIA_STATE = { NONE: "none", DOWNLOADING: "downloading", READY: "ready", FAILED: "failed" }

// Media. The Daemon sends paths only on request (media.open). `mediaPaths` holds them by Chat and Message id.
// An empty path means the request is out. Views show the path only when it is not empty.
function mediaKey(chat, id) {
  return chat + "|" + id
}

function mediaPathEntry(result) {
  var entry = {}
  entry[mediaKey(result.chat, result.id)] = result.path
  return entry
}

function mediaPath(state, message) {
  return (state.mediaPaths || {})[mediaKey(message.chat, message.id)] || ""
}

// A sticker shows inside the pop-out once the Daemon holds it. An image shows only as the thumbnail of its chip.
function showsInline(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY && message.media.kind === "sticker"
}

// A ready image without WhatsApp's thumbnail: its chip shows the downloaded file as the thumbnail.
function previewsFile(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY && message.media.kind === "image" && !message.media.thumbnail
}

// The Messages of the open Chat whose path the Plugin must ask for. markMediaRequested then stops a second request.
function mediaToLoad(state) {
  if (!state.openChat) return []
  var known = state.mediaPaths || {}
  return state.openChat.messages.filter(function(m) { return (showsInline(m) || previewsFile(m)) && known[mediaKey(m.chat, m.id)] === undefined })
}

function markMediaRequested(state, messages) {
  var entries = {}
  messages.forEach(function(m) { entries[mediaKey(m.chat, m.id)] = "" })
  return Common.withChanges(state, { mediaPaths: Common.withChanges(state.mediaPaths || {}, entries) })
}

var MEDIA_KIND_LABELS = { image: "Image", sticker: "Sticker", voice: "Voice note", video: "Video", audio: "Audio", document: "Document" }

// The one line that stands for Media in the timeline, for example "Document: report.pdf (1.2 MB)".
function mediaLabel(message) {
  var media = message.media
  if (!media) return ""
  var label = media.gif ? "GIF" : MEDIA_KIND_LABELS[media.kind] || "File"
  if (media.name) label += ": " + media.name
  return label + " (" + (media.duration ? formatDuration(media.duration) + ", " : "") + formatSize(media.size) + ")"
}

// Seconds as "m:ss", for the length of audio, voice and video.
function formatDuration(seconds) {
  var s = Math.round(seconds)
  return Math.floor(s / 60) + ":" + (s % 60 < 10 ? "0" : "") + (s % 60)
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + " B"
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB"
  return (bytes / (1024 * 1024)).toFixed(1) + " MB"
}

// The state text of Media that is not ready: "" when there is nothing to say.
function mediaStatus(message) {
  if (!message.media) return ""
  if (message.media.state === MEDIA_STATE.DOWNLOADING) return "Downloading…"
  return message.media.state === MEDIA_STATE.FAILED ? "Download failed" : ""
}

function isDownloading(message) {
  return !!message.media && message.media.state === MEDIA_STATE.DOWNLOADING
}

function canDownloadMedia(message) {
  return !!message.media && (message.media.state === MEDIA_STATE.NONE || message.media.state === MEDIA_STATE.FAILED)
}

function canOpenMedia(message) {
  return !!message.media && message.media.state === MEDIA_STATE.READY
}

// mpv detects a playlist by content, so a sender could make it fetch a URL. These flags make mpv read the file as media
// and only from disk.
var MPV_LOCAL_ONLY = ["--demuxer=lavf", "--demuxer-lavf-o=protocol_whitelist=file", "--ytdl=no", "--"]

// The command that opens a plain copy: video in mpv, a GIF in mpv on a loop, a voice note or audio in mpv without a window,
// everything else in the default app.
function openCommand(message, path) {
  var kind = message.media.kind
  var flag = kind === "voice" || kind === "audio" ? ["--no-video"] : message.media.gif ? ["--loop"] : []
  if (flag.length === 0 && kind !== "video") return ["xdg-open", path]
  return ["mpv"].concat(flag, MPV_LOCAL_ONLY, [path])
}

// xdg-open picks the app from the file content (`file --mime-type`), not from the extension. So the Plugin probes the
// same way and asks first unless the content type is on this short safe list. Any other type, such as text/html,
// image/svg+xml or a script, can run active content in the default app.
var SAFE_MIMES = ["application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "text/plain", "text/csv",
  "application/rtf", "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint",
  "application/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.spreadsheet", "application/vnd.oasis.opendocument.presentation"]

// True when the default app opens this Message (mpv opens the rest, hardened).
function opensInDefaultApp(message, path) {
  return openCommand(message, path)[0] === "xdg-open"
}

// The probe that gives the content type, the same way xdg-open does.
function mimeProbeCommand(path) {
  return ["file", "--brief", "--dereference", "--mime-type", "--", path]
}

// True when `mime` (the output of the probe) is safe to open without a question. An empty answer, from a probe error, is not.
function mimeIsSafe(mime) {
  var type = String(mime || "").trim().toLowerCase()
  return SAFE_MIMES.indexOf(type) !== -1 || type.indexOf("application/vnd.openxmlformats-officedocument.") === 0
}

// Voice notes play in the pop-out. `playingKey` is the mediaKey of the note that plays now, or "".
function isVoice(message) {
  return !!message.media && message.media.kind === "voice"
}

function isPlaying(playingKey, message) {
  return playingKey !== "" && playingKey === mediaKey(message.chat, message.id)
}

// The label of the button that opens Media: Play or Stop for a voice note, Open for the rest.
function openLabel(message, playingKey) {
  if (!isVoice(message)) return "Open"
  return isPlaying(playingKey, message) ? "Stop" : "Play"
}

// The Daemon sends Played (and Read for that Message in On interaction mode) when playback starts.
function playedRequest(id, message) {
  return Common.request(id, "media.played", { chat: message.chat, id: message.id })
}

function mediaDownloadRequest(id, message) {
  return Common.request(id, "media.download", { chat: message.chat, id: message.id })
}

function mediaOpenRequest(id, message) {
  return Common.request(id, "media.open", { chat: message.chat, id: message.id })
}

// The path the Save as field starts with: ~/Downloads and the file name. Media without a name gets "<kind>-<id>"
// and an extension from its MIME type. A "/" in a name cannot leave the folder.
// shortcut: always ~/Downloads, not XDG_DOWNLOAD_DIR; read user-dirs.dirs when someone asks.
function defaultSavePath(message) {
  var media = message.media || {}
  var name = String(media.name || "").replace(/\//g, "_").trim()
  if (!name) {
    var sub = String(media.mime || "").split(";")[0].split("/")[1] || ""
    var ext = sub === "jpeg" ? "jpg" : /^[a-z0-9]+$/.test(sub) ? sub : ""
    name = (media.kind || "file") + "-" + String(message.id).replace(/[^A-Za-z0-9_-]/g, "") + (ext ? "." + ext : "")
  }
  return "~/Downloads/" + name
}

// The typed Save as path with a leading "~" made absolute. The Daemon takes absolute paths only.
function savePath(text, home) {
  var path = String(text).trim()
  if (path === "~" || path.indexOf("~/") === 0) return home + path.substring(1)
  return path
}

// `path` is the file the user chose in the Save as field, made absolute by savePath.
function mediaSaveRequest(id, message, path) {
  return Common.request(id, "media.save", { chat: message.chat, id: message.id, path: path })
}

// The Plugin sends this when a Chat closes, so the Daemon deletes the plain copies.
function mediaReleaseRequest(id, jid) {
  return Common.request(id, "media.release", { chat: jid })
}

// True when the download of the Media of a Message failed.
function mediaFailed(message) {
  return !!message.media && message.media.state === MEDIA_STATE.FAILED
}

function downloadLabel(message) {
  return mediaFailed(message) ? "Retry download" : "Download"
}

// What to do when the user asks to open Media: "stop" the playing note, "launch" at once (path known), or "request" the path.
function openMediaStep(state, playingKey, message) {
  if (isPlaying(playingKey, message)) return { action: "stop", path: "" }
  var path = mediaPath(state, message)
  return path !== "" ? { action: "launch", path: path } : { action: "request", path: "" }
}

// Media the user clicked to open, by mediaKey, until its path arrives. These two keep that table without changing it.
function rememberOpen(pending, message) {
  var entry = {}
  entry[mediaKey(message.chat, message.id)] = message
  return Common.withChanges(pending, entry)
}

// A media.open result: the Message that waits for this path (or null), and the table without it.
function takeOpen(pending, result) {
  var key = mediaKey(result.chat, result.id)
  if (!pending[key]) return { message: null, pending: pending }
  var rest = {}
  for (var k in pending) if (k !== key) rest[k] = pending[k]
  return { message: pending[key], pending: rest }
}

// What a Daemon frame means for Media. Returns { state, pending, launch, load }:
// launch is { message, path } for Media the user clicked and whose path just arrived, or null;
// load lists the Messages whose path the Plugin must ask for now (state already marks them as asked).
function mediaFrameStep(state, pending, frame) {
  var result = frame.result
  var launch = null
  if (result && result.path) {
    var taken = takeOpen(pending, result)
    pending = taken.pending
    if (taken.message) launch = { message: taken.message, path: result.path }
  }
  var load = mediaToLoad(state)
  if (load.length > 0) state = markMediaRequested(state, load)
  return { state: state, pending: pending, launch: launch, load: load }
}

// Media chips. A ready sticker shows as itself, so it gets no chip.
function chipShown(m) {
  return !!m.media && !(m.media.kind === "sticker" && m.media.state === MEDIA_STATE.READY)
}

// WhatsApp's thumbnail as a data URL. Without one, the downloaded file of an image (`path`, from mediaPath), else "".
function thumbnailSource(m, path) {
  if (!m.media) return ""
  if (m.media.thumbnail) return "data:image/jpeg;base64," + m.media.thumbnail
  return previewsFile(m) && path ? fileUrl(path) : ""
}

// The file: URL of an absolute path. Each segment is encoded, so a "#" or "?" in a name stays part of the path.
function fileUrl(path) {
  return "file://" + path.split("/").map(encodeURIComponent).join("/")
}

// A video that is not downloaded shows its thumbnail blurred.
function chipBlurred(m) {
  return !!m.media && m.media.kind === "video" && m.media.state !== MEDIA_STATE.READY
}

var MIME_BADGES = { "application/pdf": "PDF", "audio/mpeg": "MP3", "audio/ogg": "OGG", "audio/mp4": "M4A", "audio/wav": "WAV", "audio/flac": "FLAC" }

// The file-type badge of a chip without a thumbnail. An image, video or GIF says IMG, VID or GIF.
// A document or audio file shows the extension of the name, else the MIME type, else the kind.
function fileBadge(m) {
  var md = m.media
  if (!md || md.thumbnail) return ""
  if (md.kind === "image") return "IMG"
  if (md.kind === "video") return md.gif ? "GIF" : "VID"
  if (md.kind !== "document" && md.kind !== "audio") return ""
  var ext = /\.([^.]+)$/.exec(md.name || "")
  if (ext) return ext[1].toUpperCase()
  return MIME_BADGES[String(md.mime).split(";")[0]] || (md.kind === "audio" ? "AUDIO" : "FILE")
}

// The action of a click on the thumbnail or the name: download, play or open. "" while downloading and for a sticker.
function chipMainAction(m) {
  var a = Message.actionsFor(m)
  var main = ["download", "play", "open"].filter(function(x) { return a.indexOf(x) >= 0 })
  return main.length > 0 ? main[0] : ""
}

var CHIP_LINKS = ["download", "play", "open", "saveAs"]

// The links under the chip, from the actions rule. A voice note has a play button instead of a Play link.
function chipLinks(m) {
  var failed = !!m.media && m.media.state === MEDIA_STATE.FAILED
  return Message.actionsFor(m).filter(function(x) { return CHIP_LINKS.indexOf(x) >= 0 && !(x === "play" && m.media.kind === "voice") }).map(function(x) {
    return { action: x, label: x === "download" && failed ? "retry" : Message.ACTION_LABELS[x].toLowerCase() }
  })
}

// A received voice note that is ready and not played yet. The Daemon sets `media.played` on the first play.
function unplayedDot(m) {
  return !!m.media && m.media.kind === "voice" && m.media.state === MEDIA_STATE.READY && m.fromMe !== true && m.media.played !== true
}
