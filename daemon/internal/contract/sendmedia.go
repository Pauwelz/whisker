package contract

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// maxSendBytes is the largest file `chat.sendMedia` accepts.
const maxSendBytes = 100 << 20

// sendableImages are the types that go out as an image. Every other file goes out as a document.
var sendableImages = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// sniffMime names the type of a file from its extension, else from its first bytes.
func sniffMime(path string, f *os.File) string {
	t := mime.TypeByExtension(filepath.Ext(path))
	if t == "" {
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		t = http.DetectContentType(head[:n])
		f.Seek(0, io.SeekStart)
	}
	t, _, _ = strings.Cut(t, ";")
	return t
}

// chatSendMediaReply stores the file encrypted in the Profile and adds a pending Message. The caller sends it after the reply.
// shortcut: the copy into the Profile runs on the Daemon loop, so a very large file blocks other requests for a moment.
func chatSendMediaReply(chats *chatstore.Store, store *media.Store, newID func() string, id json.RawMessage, chat, path, caption string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	invalid := errorReply(id, errMessage, msgInvalidMessage)
	if !filepath.IsAbs(path) || !fitsMessage(caption) {
		return invalid, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return invalid, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxSendBytes {
		return invalid, nil
	}
	m := whatsapp.Media{Kind: media.Document, Mime: sniffMime(path, f), Name: filepath.Base(path), Size: st.Size(), Key: make([]byte, 32)}
	if sendableImages[m.Mime] {
		m.Kind, m.Name = media.Image, ""
	}
	if _, err := rand.Read(m.Key); err != nil {
		fmt.Fprintln(logOut, "media copy failed")
		return errorReply(id, errMediaFailed, msgMediaCopy), nil
	}
	msgID := newID()
	if err := store.Put(chat, msgID, m.Kind, m.Key, f); err != nil {
		fmt.Fprintln(logOut, "media copy failed") // the error text could hold paths
		return errorReply(id, errMediaFailed, msgMediaCopy), nil
	}
	msg, err := chats.AddPendingMedia(chat, msgID, caption, &m)
	if err != nil {
		store.Remove(chat, msgID)
	}
	return messageReply(id, msg, err, logOut)
}
