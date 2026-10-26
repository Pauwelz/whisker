package contract

import (
	"bytes"
	"crypto/rand"
	"errors"
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
	"golang.org/x/sys/unix"
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

// openSendable opens a file that `chat.sendMedia` accepts: an absolute path to a regular file of 1 B to 100 MiB.
// No part of the path may be a symlink. The checks run on the open file, so a swap after the open changes nothing.
// The open never blocks: a FIFO or a device fails the regular-file check.
func openSendable(path string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, errors.New("not absolute")
	}
	const flags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_SYMLINKS})
	var f *os.File
	switch {
	case err == nil:
		f = os.NewFile(uintptr(fd), path)
	case errors.Is(err, unix.ENOSYS): // an old kernel: O_NOFOLLOW still refuses a symlink in the last part
		f, err = os.OpenFile(path, flags, 0)
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxSendBytes {
		f.Close()
		return nil, nil, errors.New("not sendable")
	}
	return f, st, nil
}

// statSendable checks a path with openSendable. `draft.set` and `draft.get` use it too, so all agree on what can be sent.
func statSendable(path string) (os.FileInfo, error) {
	f, st, err := openSendable(path)
	if err != nil {
		return nil, err
	}
	f.Close()
	return st, nil
}

// resolveSendable gives the real path of a new Attachment, if openSendable accepts it. The Draft stores that path.
func resolveSendable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("not absolute")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if _, err := statSendable(real); err != nil {
		return "", err
	}
	return real, nil
}

// errMediaCopy marks a file that could not be copied into the Media store.
var errMediaCopy = errors.New("media copy failed")

// errBadImage marks an Image that does not parse as its type, so its metadata cannot be stripped.
var errBadImage = errors.New("image not sendable")

// chatSendMedia checks every path, then stores each file encrypted in the Profile as a pending Message, in order.
// Only the first Message has the caption. The follow-up sends them after the reply and clears the Draft once.
// It is all or nothing: one bad path gives error -32013 with `error.data.paths`, and a failure while storing deletes the Messages already stored.
// shortcut: the copy into the Profile runs on the Daemon loop, so a very large file blocks other requests for a moment.
func (s *session) chatSendMedia(req *request) outcome {
	p := req.Params
	invalid := outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	if !fitsMessage(p.Caption) || len(p.Paths) == 0 || len(p.Paths) > maxAttachments {
		return invalid
	}
	if bad := unsendable(p.Paths); len(bad) > 0 {
		return outcome{reply: errorReplyData(req.ID, errNotSendable, msgNotSendable, map[string]any{"paths": bad})}
	}
	var sent []chatstore.Message
	for i, path := range p.Paths {
		caption := ""
		if i == 0 {
			caption = p.Caption
		}
		m, err := s.storeMedia(p.Chat, path, caption)
		if err != nil {
			for _, done := range sent {
				if err := s.chats.DeleteMessage(p.Chat, done.ID); err != nil {
					fmt.Fprintln(s.logOut, "message delete failed")
				}
			}
			if errors.Is(err, errBadImage) {
				return outcome{reply: errorReplyData(req.ID, errNotSendable, msgNotSendable, map[string]any{"paths": []string{path}})}
			}
			if errors.Is(err, errMediaCopy) {
				return outcome{reply: errorReply(req.ID, errMediaFailed, msgMediaCopy)}
			}
			reply, _ := s.messageReply(req.ID, m, err)
			return outcome{reply: reply}
		}
		sent = append(sent, m)
	}
	for _, path := range p.Paths {
		dropPasted(path) // the Media store holds the encrypted copies now
	}
	return s.sentOutcome(p.Chat, resultReply(req.ID, map[string]any{"sent": sent}), sent)
}

// storeMedia copies one checked file into the Media store and adds its pending Message.
// An Image loses its metadata first (Orientation stays), so the stored copy, the upload and the hash hold the stripped bytes. A Document stays byte-exact.
func (s *session) storeMedia(chat, path, caption string) (chatstore.Message, error) {
	f, st, err := openSendable(path)
	if err != nil {
		return chatstore.Message{}, errMediaCopy
	}
	defer f.Close()
	m := whatsapp.Media{Kind: media.Document, Mime: sniffMime(path, f), Name: filepath.Base(path), Size: st.Size(), Key: make([]byte, 32)}
	if sendableImages[m.Mime] {
		m.Kind, m.Name = media.Image, ""
	}
	var plain io.Reader = io.LimitReader(f, maxSendBytes+1) // a file that grew since the open is cut
	if m.Kind == media.Image {
		raw, err := io.ReadAll(io.LimitReader(f, maxSendBytes+1))
		if err != nil || len(raw) > maxSendBytes {
			return chatstore.Message{}, errMediaCopy
		}
		stripped, err := media.StripMetadata(m.Mime, raw)
		if err != nil {
			return chatstore.Message{}, errBadImage
		}
		m.Size, plain = int64(len(stripped)), bytes.NewReader(stripped)
	}
	if _, err := rand.Read(m.Key); err != nil {
		fmt.Fprintln(s.logOut, "media copy failed")
		return chatstore.Message{}, errMediaCopy
	}
	msgID := s.port.NewMessageID()
	if err := s.media.Put(chat, msgID, m.Kind, m.Key, plain); err != nil {
		fmt.Fprintln(s.logOut, "media copy failed") // the error text could hold paths
		return chatstore.Message{}, errMediaCopy
	}
	msg, err := s.chats.AddPendingMedia(chat, msgID, caption, &m)
	if err != nil {
		s.media.Remove(chat, msgID)
	}
	return msg, err
}
