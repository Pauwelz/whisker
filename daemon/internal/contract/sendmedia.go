package contract

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

// statSendable checks a file that `chat.sendMedia` accepts: an absolute path to a regular file of 1 B to 100 MiB.
// `draft.set` and `draft.get` use it too, so all agree on what can be sent. It never opens the file, so a FIFO cannot block the loop.
func statSendable(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("not absolute")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxSendBytes {
		return nil, errors.New("not sendable")
	}
	return st, nil
}

// chatSendMedia stores the file encrypted in the Profile and adds a pending Message. The follow-up sends it after the reply.
// shortcut: the copy into the Profile runs on the Daemon loop, so a very large file blocks other requests for a moment.
func (s *session) chatSendMedia(req *request) outcome {
	p := req.Params
	invalid := outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	if !fitsMessage(p.Caption) {
		return invalid
	}
	st, err := statSendable(p.Path)
	if err != nil {
		return invalid
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return invalid
	}
	defer f.Close()
	m := whatsapp.Media{Kind: media.Document, Mime: sniffMime(p.Path, f), Name: filepath.Base(p.Path), Size: st.Size(), Key: make([]byte, 32)}
	if sendableImages[m.Mime] {
		m.Kind, m.Name = media.Image, ""
	}
	if _, err := rand.Read(m.Key); err != nil {
		fmt.Fprintln(s.logOut, "media copy failed")
		return outcome{reply: errorReply(req.ID, errMediaFailed, msgMediaCopy)}
	}
	msgID := s.port.NewMessageID()
	if err := s.media.Put(p.Chat, msgID, m.Kind, m.Key, f); err != nil {
		fmt.Fprintln(s.logOut, "media copy failed") // the error text could hold paths
		return outcome{reply: errorReply(req.ID, errMediaFailed, msgMediaCopy)}
	}
	msg, err := s.chats.AddPendingMedia(p.Chat, msgID, p.Caption, &m)
	if err != nil {
		s.media.Remove(p.Chat, msgID)
	}
	reply, submit := s.messageReply(req.ID, msg, err)
	return s.sentOutcome(p.Chat, reply, submit)
}
