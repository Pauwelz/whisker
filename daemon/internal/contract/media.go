package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// maxParallelDownloads is the number of full Media files, and of separate thumbnails, that download at the same time.
const maxParallelDownloads = 3

// downloadPort is what the mediaLoader needs from WhatsApp.
type downloadPort interface {
	Download(ref whatsapp.Media, dst *os.File, limit int64) error
	DownloadThumbnail(ref whatsapp.Media) ([]byte, error)
}

// mediaLoader downloads Media and hands out paths to plain copies.
// Images, stickers and voice notes download when the Message arrives (the chat store marks them downloading).
// Other Media downloads on `media.download`. It runs on the Daemon loop; only the downloads run on other goroutines.
// Listed Media without an inline thumbnail gets a preview: WhatsApp's separate thumbnail, or else, for an image, the file.
// Logs hold fixed texts only: never a Chat, a name or a path.
type mediaLoader struct {
	chats    *chatstore.Store
	port     downloadPort
	store    *media.Store
	notify   func(chat, id string) bool
	logOut   io.Writer
	inflight map[string]bool
	results  chan mediaResult
	// thumbTried holds the thumbnails fetched in this run, done or not. An oversized one loses its link in the store.
	thumbTried    map[string]bool
	thumbSlots    chan struct{}         // limits the thumbnail downloads that run at once
	fileSlots     chan struct{}         // limits the full downloads that run at once
	autoDownloads func(size int64) bool // settings.Settings.AutoDownloads of the current settings
}

// mediaResult ends a download of a file, or of a thumbnail when thumbnail is set.
type mediaResult struct {
	chat, id  string
	err       error
	thumbnail bool
	data      []byte // the thumbnail
}

// newMediaLoader wipes the plain copies of an earlier run and makes interrupted downloads startable again.
func newMediaLoader(chats *chatstore.Store, port downloadPort, store *media.Store, notify func(chat, id string) bool, logOut io.Writer, autoDownloads func(size int64) bool) *mediaLoader {
	store.Wipe()
	storeFailed(logOut, storeWrite, chats.ResetDownloads())
	chats.RemoveMedia = store.Remove
	return &mediaLoader{chats: chats, port: port, store: store, notify: notify, logOut: logOut, inflight: map[string]bool{}, results: make(chan mediaResult, 16), thumbTried: map[string]bool{}, thumbSlots: make(chan struct{}, maxParallelDownloads), fileSlots: make(chan struct{}, maxParallelDownloads), autoDownloads: autoDownloads}
}

// start downloads the Media of a Message that is marked downloading. It does nothing while that Media is in flight.
func (l *mediaLoader) start(chat, id string) {
	ref, err := l.chats.MediaOf(chat, id)
	key := messageKey(chat, id)
	if err != nil || l.inflight[key] {
		return
	}
	l.inflight[key] = true
	go func() {
		l.fileSlots <- struct{}{}
		defer func() { <-l.fileSlots }()
		err := l.store.Ingest(chat, id, ref.Kind, func(f *os.File) error {
			return l.port.Download(ref.Media, f, whatsapp.DownloadCap(ref.Size))
		})
		l.results <- mediaResult{chat: chat, id: id, err: err}
	}()
}

// autoStart starts the download of a live Message that livePolicy stored as downloading.
func (l *mediaLoader) autoStart(ev whatsapp.NewMessage) {
	if ref, err := l.chats.MediaOf(ev.Chat, ev.Message.ID); err == nil && ref.State == chatstore.MediaDownloading {
		l.start(ev.Chat, ev.Message.ID)
	}
}

// previews gives each listed Message whose Media came without an inline thumbnail a preview.
// It fetches WhatsApp's separate thumbnail. Without one, an image that is not downloaded downloads, as a live image does;
// the Plugin then shows the file as the thumbnail.
// A page holds at most maxChatsLimit Messages. At most maxParallelDownloads of them download at once.
func (l *mediaLoader) previews(chat string, msgs []chatstore.Message) {
	for _, m := range msgs {
		key := messageKey(chat, m.ID)
		if m.Media == nil || len(m.Media.Thumbnail) > 0 || l.thumbTried[key] {
			continue
		}
		ref, err := l.chats.MediaOf(chat, m.ID)
		if err != nil {
			continue
		}
		if ref.ThumbDirectPath == "" {
			l.autoImage(chat, m.ID, ref)
			continue
		}
		l.thumbTried[key] = true
		go func(id string) {
			l.thumbSlots <- struct{}{}
			defer func() { <-l.thumbSlots }()
			data, err := l.port.DownloadThumbnail(ref.Media)
			l.results <- mediaResult{chat: chat, id: id, err: err, thumbnail: true, data: data}
		}(m.ID)
	}
}

// autoImage downloads an image that is not downloaded, when the settings allow its size. A failed one waits for a click: its retry link shows.
func (l *mediaLoader) autoImage(chat, id string, ref chatstore.MediaRef) {
	if ref.Kind != media.Image || ref.State != chatstore.MediaNone || !l.autoDownloads(ref.Size) {
		return
	}
	if changed, err := l.chats.BeginDownload(chat, id); !storeFailed(l.logOut, storeWrite, err) && changed {
		l.start(chat, id)
	}
}

// thumbnailDone stores a fetched thumbnail and tells the Plugin. It returns false when a frame could not be written.
func (l *mediaLoader) thumbnailDone(r mediaResult) bool {
	tooLarge := errors.Is(r.err, whatsapp.ErrThumbnailTooLarge) || len(r.data) > whatsapp.MaxThumbnailBytes
	if r.err != nil || len(r.data) == 0 || tooLarge {
		// An oversized link is hostile: a later run does not fetch it again. Another failure is tried in the next run.
		if tooLarge {
			storeFailed(l.logOut, storeWrite, l.chats.ClearThumbLink(r.chat, r.id))
		}
		fmt.Fprintln(l.logOut, "thumbnail download failed") // the error text could hold hosts
		return true
	}
	changed, err := l.chats.SetThumbnail(r.chat, r.id, r.data)
	if storeFailed(l.logOut, storeWrite, err) {
		return true
	}
	return !changed || l.notify(r.chat, r.id)
}

// done ends a download. It returns false when a frame could not be written.
func (l *mediaLoader) done(r mediaResult) bool {
	if r.thumbnail {
		return l.thumbnailDone(r)
	}
	delete(l.inflight, messageKey(r.chat, r.id))
	state := chatstore.MediaReady
	if r.err != nil {
		fmt.Fprintln(l.logOut, "media download failed") // the error text could hold hosts or paths
		state = chatstore.MediaFailed
	}
	changed, err := l.chats.EndDownload(r.chat, r.id, state)
	if storeFailed(l.logOut, storeWrite, err) {
		return true
	}
	if !changed {
		l.store.Remove(r.chat, r.id) // the Message was revoked meanwhile: keep no file
		return true
	}
	return l.notify(r.chat, r.id)
}

// downloadReply handles `media.download`. The caller runs start after the reply.
func (l *mediaLoader) downloadReply(id json.RawMessage, chat, msgID string) (map[string]any, bool) {
	changed, err := l.chats.BeginDownload(chat, msgID)
	switch {
	case storeFailed(l.logOut, storeWrite, err):
		return errorReply(id, errChatStore, msgChatStore), false
	case !changed:
		return errorReply(id, errMedia, msgInvalidMedia), false
	}
	m, err := l.chats.Message(chat, msgID)
	if storeFailed(l.logOut, storeRead, err) {
		return errorReply(id, errChatStore, msgChatStore), false
	}
	return resultReply(id, map[string]any{"message": m}), true
}

// ready returns the Media of a Message when its file is stored.
func (l *mediaLoader) ready(chat, id string) (chatstore.MediaRef, bool) {
	ref, err := l.chats.MediaOf(chat, id)
	return ref, err == nil && ref.State == chatstore.MediaReady && l.store.Has(chat, id)
}

// openReply handles `media.open`: it writes a plain copy to the runtime folder and answers with its path.
func (l *mediaLoader) openReply(id json.RawMessage, chat, msgID string) map[string]any {
	ref, ok := l.ready(chat, msgID)
	if !ok {
		return errorReply(id, errMedia, msgInvalidMedia)
	}
	// shortcut: the decrypt runs on the Daemon loop, so a very large video blocks other requests for a moment.
	path, err := l.store.Plain(chat, msgID, extension(ref.Media), ref.Kind)
	if err != nil {
		return l.failed(id, err)
	}
	return resultReply(id, map[string]any{"chat": chat, "id": msgID, "path": path})
}

// saveReply handles `media.save`: it exports a plain copy to a path the user chose.
func (l *mediaLoader) saveReply(id json.RawMessage, chat, msgID, dst string) map[string]any {
	ref, ok := l.ready(chat, msgID)
	if !ok || !filepath.IsAbs(dst) {
		return errorReply(id, errMedia, msgInvalidMedia)
	}
	if err := l.store.Export(chat, msgID, ref.Kind, dst); err != nil {
		return l.failed(id, err)
	}
	return resultReply(id, map[string]any{})
}

// playedReply handles `media.played`: the user started a voice note. Only a ready voice note of another Participant counts.
// The first play stores the mark. The Message comes back so the caller can tell the Plugin; it is nil for a repeat.
func (l *mediaLoader) playedReply(id json.RawMessage, chat, msgID string, read *reader) (map[string]any, *chatstore.Message) {
	ref, ok := l.ready(chat, msgID)
	m, err := l.chats.Message(chat, msgID)
	if !ok || err != nil || ref.Kind != media.Voice || m.FromMe {
		return errorReply(id, errMedia, msgInvalidMedia), nil
	}
	read.playedNote(chat, m.Sender, msgID)
	fresh, err := l.chats.SetPlayed(chat, msgID)
	if err != nil {
		fmt.Fprintln(l.logOut, "media write failed")
		return errorReply(id, errChatStore, msgChatStore), nil
	}
	if !fresh {
		return resultReply(id, map[string]any{}), nil
	}
	m.Media.Played = true
	return resultReply(id, map[string]any{}), &m
}

func (l *mediaLoader) failed(id json.RawMessage, err error) map[string]any {
	if errors.Is(err, media.ErrIntegrity) {
		fmt.Fprintln(l.logOut, "media failed the integrity check")
		return errorReply(id, errMediaFailed, msgMediaIntegrity)
	}
	fmt.Fprintln(l.logOut, "media copy failed") // the error text could hold paths
	return errorReply(id, errMediaFailed, msgMediaCopy)
}

// extension is the file extension for a plain copy. Other apps use it to pick the type.
func extension(m whatsapp.Media) string {
	if ext := filepath.Ext(m.Name); ext != "" {
		return ext
	}
	mime, _, _ := strings.Cut(m.Mime, ";")
	return mimeExtensions[strings.TrimSpace(mime)]
}

var mimeExtensions = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "audio/ogg": ".ogg", "audio/mpeg": ".mp3", "audio/mp4": ".m4a", "application/pdf": ".pdf",
}
