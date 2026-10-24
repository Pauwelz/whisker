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

// mediaLoader downloads Media and hands out paths to plain copies.
// Images, stickers and voice notes download when the Message arrives (the chat store marks them downloading).
// Other Media downloads on `media.download`. It runs on the Daemon loop; only the downloads run on other goroutines.
// Logs hold fixed texts only: never a Chat, a name or a path.
type mediaLoader struct {
	chats    *chatstore.Store
	port     whatsapp.Port
	store    *media.Store
	notify   func(chat, id string) bool
	logOut   io.Writer
	inflight map[string]bool
	results  chan mediaResult
}

type mediaResult struct {
	chat, id string
	err      error
}

// newMediaLoader wipes the plain copies of an earlier run and makes interrupted downloads startable again.
func newMediaLoader(chats *chatstore.Store, port whatsapp.Port, store *media.Store, notify func(chat, id string) bool, logOut io.Writer) *mediaLoader {
	store.Wipe()
	if err := chats.ResetDownloads(); err != nil {
		fmt.Fprintln(logOut, "chat store write failed")
	}
	chats.RemoveMedia = store.Remove
	return &mediaLoader{chats: chats, port: port, store: store, notify: notify, logOut: logOut, inflight: map[string]bool{}, results: make(chan mediaResult, 16)}
}

// start downloads the Media of a Message that is marked downloading. It does nothing while that Media is in flight.
func (l *mediaLoader) start(chat, id string) {
	ref, err := l.chats.MediaOf(chat, id)
	key := chat + "\x00" + id
	if err != nil || l.inflight[key] {
		return
	}
	l.inflight[key] = true
	go func() {
		err := l.store.Ingest(chat, id, ref.Kind, ref.Key, func(f *os.File) error { return l.port.Download(ref.Media, f) })
		l.results <- mediaResult{chat, id, err}
	}()
}

// autoStart starts the download of a live Message whose Media downloads by itself.
func (l *mediaLoader) autoStart(ev whatsapp.NewMessage) {
	if m := ev.Message.Media; m != nil && m.Kind.Auto() {
		l.start(ev.Chat, ev.Message.ID)
	}
}

// done ends a download. It returns false when a frame could not be written.
func (l *mediaLoader) done(r mediaResult) bool {
	delete(l.inflight, r.chat+"\x00"+r.id)
	state := chatstore.MediaReady
	if r.err != nil {
		fmt.Fprintln(l.logOut, "media download failed") // the error text could hold hosts or paths
		state = chatstore.MediaFailed
	}
	changed, err := l.chats.EndDownload(r.chat, r.id, state)
	if err != nil {
		fmt.Fprintln(l.logOut, "chat store write failed")
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
	case err != nil:
		fmt.Fprintln(l.logOut, "chat store write failed")
		return errorReply(id, errChatStore, "chat store failed"), false
	case !changed:
		return errorReply(id, errMedia, "invalid media"), false
	}
	m, err := l.chats.Message(chat, msgID)
	if err != nil {
		fmt.Fprintln(l.logOut, "chat store read failed")
		return errorReply(id, errChatStore, "chat store failed"), false
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
		return errorReply(id, errMedia, "invalid media")
	}
	// shortcut: the decrypt runs on the Daemon loop, so a very large video blocks other requests for a moment.
	path, err := l.store.Plain(chat, msgID, extension(ref.Media), ref.Kind, ref.Key)
	if err != nil {
		return l.failed(id, err)
	}
	return resultReply(id, map[string]any{"chat": chat, "id": msgID, "path": path})
}

// saveReply handles `media.save`: it exports a plain copy to a path the user chose.
func (l *mediaLoader) saveReply(id json.RawMessage, chat, msgID, dst string) map[string]any {
	ref, ok := l.ready(chat, msgID)
	if !ok || !filepath.IsAbs(dst) {
		return errorReply(id, errMedia, "invalid media")
	}
	if err := l.store.Export(chat, msgID, ref.Kind, ref.Key, dst); err != nil {
		return l.failed(id, err)
	}
	return resultReply(id, map[string]any{})
}

func (l *mediaLoader) failed(id json.RawMessage, err error) map[string]any {
	if errors.Is(err, media.ErrIntegrity) {
		fmt.Fprintln(l.logOut, "media failed the integrity check")
		return errorReply(id, errMediaFailed, "media failed the integrity check")
	}
	fmt.Fprintln(l.logOut, "media copy failed") // the error text could hold paths
	return errorReply(id, errMediaFailed, "media copy failed")
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
