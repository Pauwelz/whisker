// Package media keeps Media in the Profile in WhatsApp's CDN format and hands out plain copies on request.
// A stored file is AES-256-CBC ciphertext followed by a 10-byte truncated HMAC-SHA256, keyed by the mediaKey.
// Plain copies live only in a private runtime folder (tmpfs). Wipe clears it at start and exit.
// Callers must never log a path from this package.
package media

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
)

// Kind is the Media type. It selects the key derivation info.
type Kind string

const (
	Image    Kind = "image"
	Sticker  Kind = "sticker"
	Voice    Kind = "voice"
	Video    Kind = "video"
	Audio    Kind = "audio"
	Document Kind = "document"
)

// Auto reports whether the Daemon downloads this kind without a click.
func (k Kind) Auto() bool { return k == Image || k == Sticker || k == Voice }

// Info is the HKDF info string, and the whatsmeow MediaType. WhatsApp uses the image keys for stickers and the audio keys for voice notes.
func (k Kind) Info() string {
	switch k {
	case Image, Sticker:
		return "WhatsApp Image Keys"
	case Voice, Audio:
		return "WhatsApp Audio Keys"
	case Video:
		return "WhatsApp Video Keys"
	}
	return "WhatsApp Document Keys"
}

const macLength = 10

// ErrIntegrity means the stored file failed its HMAC check, or its key does not fit.
var ErrIntegrity = errors.New("media failed the integrity check")

var errName = errors.New("unsafe media name")

var errNoRuntime = errors.New("no runtime folder")

// getMediaKeys re-implements the unexported whatsmeow helper: HKDF-SHA256 of the mediaKey, 112 bytes.
func getMediaKeys(mediaKey []byte, k Kind) (iv, cipherKey, macKey []byte) {
	x := hkdfutil.SHA256(mediaKey, nil, []byte(k.Info()), 112)
	return x[:16], x[16:48], x[48:80]
}

// Store is the Media of one Profile. dir holds the encrypted files, plainDir the plain copies.
type Store struct{ dir, plainDir string }

func New(dir, plainDir string) *Store { return &Store{dir: dir, plainDir: plainDir} }

// safe rejects names that could leave their folder.
func safe(names ...string) error {
	for _, n := range names {
		if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
			return errName
		}
	}
	return nil
}

func (s *Store) file(chat, id string) string { return filepath.Join(s.dir, chat, id) }

// Has reports whether the Media is stored.
func (s *Store) Has(chat, id string) bool {
	if safe(chat, id) != nil {
		return false
	}
	_, err := os.Stat(s.file(chat, id))
	return err == nil
}

// Put encrypts plain into the Profile. A failed Put stores nothing.
func (s *Store) Put(chat, id string, k Kind, mediaKey []byte, plain io.Reader) error {
	if err := safe(chat, id); err != nil {
		return err
	}
	dir := filepath.Dir(s.file(chat, id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	iv, cipherKey, macKey := getMediaKeys(mediaKey, k)
	if _, _, _, _, err := cbcutil.EncryptStream(cipherKey, iv, macKey, plain, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.file(chat, id))
}

// Ingest lets fetch write the plain Media to a temp file in the runtime folder, then stores it encrypted.
// The temp file is removed in every case.
func (s *Store) Ingest(chat, id string, k Kind, mediaKey []byte, fetch func(*os.File) error) error {
	if err := safe(chat, id); err != nil {
		return err
	}
	if !filepath.IsAbs(s.plainDir) {
		return errNoRuntime
	}
	if err := os.MkdirAll(s.plainDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.plainDir, ".ingest-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := fetch(tmp); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return s.Put(chat, id, k, mediaKey, tmp)
}

// Plain writes a plain copy to the runtime folder and returns its path.
// ext (for example ".jpg") helps other apps pick the type. On any error no plain copy stays.
func (s *Store) Plain(chat, id, ext string, k Kind, mediaKey []byte) (string, error) {
	if err := safe(chat, id); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.plainDir) {
		return "", errNoRuntime
	}
	if err := os.MkdirAll(filepath.Join(s.plainDir, chat), 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.plainDir, chat, id+ext)
	if err := s.Export(chat, id, k, mediaKey, path); err != nil {
		return "", err
	}
	return path, nil
}

// Export writes a plain copy to dst (mode 0600) after the HMAC passed. On error dst is removed.
func (s *Store) Export(chat, id string, k Kind, mediaKey []byte, dst string) (err error) {
	if err := safe(chat, id); err != nil {
		return err
	}
	src, err := os.Open(s.file(chat, id))
	if err != nil {
		return err
	}
	defer src.Close()
	iv, cipherKey, macKey := getMediaKeys(mediaKey, k)
	size, err := validate(src, iv, macKey)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(dst)
		}
	}()
	if _, err = io.Copy(out, io.NewSectionReader(src, 0, size)); err != nil {
		return err
	}
	if _, err = out.Seek(0, io.SeekStart); err != nil { // DecryptFile reads from the current position
		return err
	}
	if err = cbcutil.DecryptFile(cipherKey, iv, out); err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return nil
}

// validate re-implements the unexported whatsmeow validateMedia: HMAC over IV and ciphertext, cut to 10 bytes.
// It returns the length of the ciphertext.
func validate(f *os.File, iv, macKey []byte) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	n := st.Size() - macLength
	if n <= 0 || n%aes.BlockSize != 0 {
		return 0, ErrIntegrity
	}
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, n)); err != nil {
		return 0, err
	}
	mac := make([]byte, macLength)
	if _, err := f.ReadAt(mac, n); err != nil {
		return 0, err
	}
	if !hmac.Equal(h.Sum(nil)[:macLength], mac) {
		return 0, ErrIntegrity
	}
	return n, nil
}

// Release deletes the plain copies of a Chat. Call it when the Chat closes.
func (s *Store) Release(chat string) {
	if safe(chat) == nil {
		os.RemoveAll(filepath.Join(s.plainDir, chat))
	}
}

// Remove deletes the stored Media of one Message, for example when a Disappearing Message expires.
func (s *Store) Remove(chat, id string) {
	if safe(chat, id) != nil {
		return
	}
	os.Remove(s.file(chat, id))
	plain, _ := filepath.Glob(filepath.Join(s.plainDir, chat, id+".*"))
	for _, p := range append(plain, filepath.Join(s.plainDir, chat, id)) {
		os.Remove(p)
	}
}

var errMove = errors.New("media move failed")

// MoveChat moves the stored Media of the Chat `from` into the Chat `to`, when the two Chats merge.
// A file whose Message id `to` already has stays as it is there. The plain copies of `from` go.
// It keeps the folder of `from` and returns an error when a move failed, so no file is lost. The error holds no path.
func (s *Store) MoveChat(from, to string) error {
	if safe(from, to) != nil {
		return nil
	}
	s.Release(from)
	entries, err := os.ReadDir(filepath.Join(s.dir, from))
	if err != nil {
		return nil // no Media in the Chat
	}
	if os.MkdirAll(filepath.Join(s.dir, to), 0o700) != nil {
		return errMove
	}
	failed := false
	for _, e := range entries {
		if !s.Has(to, e.Name()) && os.Rename(s.file(from, e.Name()), s.file(to, e.Name())) != nil {
			failed = true
		}
	}
	if failed {
		return errMove
	}
	os.RemoveAll(filepath.Join(s.dir, from)) // the files that stayed go with the folder
	return nil
}

// Wipe deletes every plain copy. Call it at start and exit.
func (s *Store) Wipe() {
	if filepath.IsAbs(s.plainDir) {
		os.RemoveAll(s.plainDir)
	}
}
