package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// mediaRetryTimeout is how long the Daemon waits for the phone to upload expired Media again. After it the Media fails.
const mediaRetryTimeout = 2 * time.Minute

// maxParallelDownloads is the number of full Media files, and of separate thumbnails, that download at the same time.
const maxParallelDownloads = 3

// downloadPort is what the mediaLoader needs from WhatsApp.
type downloadPort interface {
	Download(ref whatsapp.Media, dst *os.File, limit int64) error
	RequestMediaRetry(chat, sender, id string, fromMe bool, key []byte) error
	ForgetMediaRetry(chat, id string)
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
	// awaiting holds the Media that the phone was asked to upload again, until the download of the new link ends.
	awaiting map[string]*recovery
	results  chan mediaResult
	// thumbTried holds the thumbnails fetched in this run, done or not. An oversized one loses its link in the store.
	thumbTried    map[string]bool
	thumbSlots    chan struct{}         // limits the thumbnail downloads that run at once
	fileSlots     chan struct{}         // limits the full downloads that run at once
	autoDownloads func(size int64) bool // settings.Settings.AutoDownloads of the current settings
}

// recovery is one wait for the phone. A Media that has one does not ask the phone a second time.
type recovery struct{ answered bool } // answered: the phone sent a new link, which downloads now

// mediaResult ends a download of a file, or of a thumbnail when thumbnail is set.
// With waited set it is no download: the phone did not answer that recovery in time.
type mediaResult struct {
	chat, id  string
	err       error
	waited    *recovery
	encSHA256 []byte // the FileEncSHA256 of the Media that was downloaded
	thumbnail bool
	data      []byte // the thumbnail
}

// newMediaLoader wipes the plain copies of an earlier run and makes interrupted downloads startable again.
func newMediaLoader(chats *chatstore.Store, port downloadPort, store *media.Store, notify func(chat, id string) bool, logOut io.Writer, autoDownloads func(size int64) bool) *mediaLoader {
	store.Wipe()
	storeFailed(logOut, storeWrite, chats.ResetDownloads())
	chats.RemoveMedia = store.Remove
	return &mediaLoader{chats: chats, port: port, store: store, notify: notify, logOut: logOut, inflight: map[string]bool{}, awaiting: map[string]*recovery{}, results: make(chan mediaResult, 16), thumbTried: map[string]bool{}, thumbSlots: make(chan struct{}, maxParallelDownloads), fileSlots: make(chan struct{}, maxParallelDownloads), autoDownloads: autoDownloads}
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
		l.results <- mediaResult{chat: chat, id: id, err: err, encSHA256: ref.FileEncSHA256}
	}()
}

// autoStart starts the download of a live Message that livePolicy stored as downloading.
func (l *mediaLoader) autoStart(chat, id string) {
	if ref, err := l.chats.MediaOf(chat, id); err == nil && ref.State == chatstore.MediaDownloading {
		l.start(chat, id)
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
	dead := whatsapp.ThumbnailLinkDead(r.err) || len(r.data) > whatsapp.MaxThumbnailBytes
	if r.err != nil || len(r.data) == 0 || dead {
		// A dead link is not fetched again in a later run. Another failure is tried in the next run.
		if dead {
			storeFailed(l.logOut, storeWrite, l.chats.ClearThumbLink(r.chat, r.id))
		}
		downloadFailed(l.logOut, "thumbnail", r.err)
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
	if r.waited != nil {
		return l.waitEnded(r)
	}
	key := messageKey(r.chat, r.id)
	asked := l.awaiting[key] != nil
	delete(l.inflight, key)
	delete(l.awaiting, key)
	if ref, err := l.chats.MediaOf(r.chat, r.id); err == nil && !bytes.Equal(ref.FileEncSHA256, r.encSHA256) {
		l.store.Remove(r.chat, r.id) // an HD child replaced the Media while it downloaded: this file is the old one
		l.autoStart(r.chat, r.id)
		return true
	}
	state := chatstore.MediaReady
	if r.err != nil {
		if errors.Is(r.err, whatsapp.ErrMediaExpired) && !asked && l.askPhone(r.chat, r.id) {
			return true
		}
		downloadFailed(l.logOut, "media", r.err)
		state = chatstore.MediaFailed
	}
	return l.end(r.chat, r.id, state)
}

// askPhone asks the phone to upload expired Media again. The Media stays downloading until the answer comes.
// It returns false when the request cannot be made.
func (l *mediaLoader) askPhone(chat, id string) bool {
	ref, err := l.chats.MediaOf(chat, id)
	m, msgErr := l.chats.Message(chat, id)
	if err != nil || msgErr != nil {
		return false
	}
	wait := &recovery{}
	l.awaiting[messageKey(chat, id)] = wait
	timer := Clock.After(mediaRetryTimeout)
	go func() {
		<-timer
		l.results <- mediaResult{chat: chat, id: id, waited: wait}
	}()
	go func() {
		if err := l.port.RequestMediaRetry(chat, m.Sender, id, m.FromMe, ref.Key); err != nil {
			l.results <- mediaResult{chat: chat, id: id, err: err}
		}
	}()
	return true
}

// waitEnded fails Media whose wait for the phone ran out. A wait that ended earlier, or that got its answer, changes nothing.
func (l *mediaLoader) waitEnded(r mediaResult) bool {
	key := messageKey(r.chat, r.id)
	if l.awaiting[key] != r.waited || r.waited.answered {
		return true
	}
	delete(l.awaiting, key)
	l.port.ForgetMediaRetry(r.chat, r.id)
	fmt.Fprintln(l.logOut, "media retry timed out")
	return l.end(r.chat, r.id, chatstore.MediaFailed)
}

// retried handles the answer of the phone. An answer for Media that nobody asked about, or that it answered already, changes nothing.
// It returns false when a frame could not be written.
func (l *mediaLoader) retried(ev whatsapp.MediaRetried) bool {
	key := messageKey(ev.Chat, ev.ID)
	wait := l.awaiting[key]
	if wait == nil || wait.answered {
		return true
	}
	if ev.NotOnPhone {
		delete(l.awaiting, key)
		return l.end(ev.Chat, ev.ID, chatstore.MediaGone)
	}
	failed := ev.DirectPath == ""
	if !failed {
		failed = storeFailed(l.logOut, storeWrite, l.chats.SetDirectPath(ev.Chat, ev.ID, ev.DirectPath))
	}
	if failed {
		delete(l.awaiting, key)
		fmt.Fprintln(l.logOut, "media retry failed")
		return l.end(ev.Chat, ev.ID, chatstore.MediaFailed)
	}
	wait.answered = true
	l.start(ev.Chat, ev.ID)
	if !l.inflight[key] { // the Message is gone
		delete(l.awaiting, key)
	}
	return true
}

// end stores the final state of a download and tells the Plugin. It returns false when a frame could not be written.
func (l *mediaLoader) end(chat, id, state string) bool {
	changed, err := l.chats.EndDownload(chat, id, state)
	if storeFailed(l.logOut, storeWrite, err) {
		return true
	}
	if !changed {
		l.store.Remove(chat, id) // the Message was revoked meanwhile: keep no file
		return true
	}
	return l.notify(chat, id)
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
