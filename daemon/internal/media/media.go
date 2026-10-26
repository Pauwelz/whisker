// Package media keeps Media in the Profile in WhatsApp's CDN format and hands out plain copies on request.
// A stored file is AES-256-CBC ciphertext followed by a 10-byte truncated HMAC-SHA256, keyed by the mediaKey.
// Plain copies live only in a private runtime folder (tmpfs). Wipe clears it at start and exit.
// Folders and files carry keyed hashes of the Chat and the Message id, so a listing shows no JID, id or time.
// Callers must never log a path from this package.
package media

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

var errNoKey = errors.New("no name key")

// NameKey derives the key for the Media names from the store key. It never uses the store key itself.
func NameKey(storeKey []byte) []byte {
	return hkdfutil.SHA256(storeKey, nil, []byte("whisker media names"), 32)
}

// getMediaKeys re-implements the unexported whatsmeow helper: HKDF-SHA256 of the mediaKey, 112 bytes.
func getMediaKeys(mediaKey []byte, k Kind) (iv, cipherKey, macKey []byte) {
	x := hkdfutil.SHA256(mediaKey, nil, []byte(k.Info()), 112)
	return x[:16], x[16:48], x[48:80]
}

// Store is the Media of one Profile. dir holds the encrypted files, plainDir the plain copies.
// Without a name key, only Wipe works.
type Store struct {
	dir, plainDir string
	nameKey       []byte
}

func New(dir, plainDir string) *Store { return &Store{dir: dir, plainDir: plainDir} }

// SetNameKey sets the key from NameKey. Profile.Open calls it before any other use.
func (s *Store) SetNameKey(k []byte) { s.nameKey = k }

func (s *Store) hash(parts ...string) string {
	h := hmac.New(sha256.New, s.nameKey)
	h.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h.Sum(nil))
}

// chatDir returns the folder name of the Chat.
func (s *Store) chatDir(chat string) (string, error) {
	if err := safe(chat); err != nil {
		return "", err
	}
	if len(s.nameKey) == 0 {
		return "", errNoKey
	}
	return s.hash("chat", chat), nil
}

// names returns the folder name of the Chat and the file name of the Message. It refuses an empty id.
func (s *Store) names(chat, id string) (dir, file string, err error) {
	if err := safe(id); err != nil {
		return "", "", err
	}
	if dir, err = s.chatDir(chat); err != nil {
		return "", "", err
	}
	return dir, s.hash("file", chat, id), nil
}

// safe rejects names that could leave their folder.
func safe(names ...string) error {
	for _, n := range names {
		if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
			return errName
		}
	}
	return nil
}

// file is the path of the stored file.
func (s *Store) file(chat, id string) (string, error) {
	d, f, err := s.names(chat, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, d, f), nil
}

// Has reports whether the Media is stored.
func (s *Store) Has(chat, id string) bool {
	p, err := s.file(chat, id)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Put encrypts plain into the Profile. A failed Put stores nothing.
func (s *Store) Put(chat, id string, k Kind, mediaKey []byte, plain io.Reader) error {
	path, err := s.file(chat, id)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return epochAll(path, dir, s.dir)
}

// epoch sets the times of a file to the Unix epoch, so they do not show when the Media arrived.
func epoch(path string) error { return os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)) }

// epochAll sets the times of the files and folders that a change touched. The change time (ctime) cannot be set.
func epochAll(paths ...string) error {
	var first error
	for _, p := range paths {
		if err := epoch(p); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

// Ingest lets fetch write the plain Media to a temp file in the runtime folder, then stores it encrypted.
// The temp file is removed in every case.
func (s *Store) Ingest(chat, id string, k Kind, mediaKey []byte, fetch func(*os.File) error) error {
	if _, _, err := s.names(chat, id); err != nil {
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
	d, f, err := s.names(chat, id)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.plainDir) {
		return "", errNoRuntime
	}
	if err := os.MkdirAll(filepath.Join(s.plainDir, d), 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.plainDir, d, f+ext)
	if err := s.Export(chat, id, k, mediaKey, path); err != nil {
		os.Remove(path) // an older copy must not stay after a failed rewrite
		return "", err
	}
	epochAll(path, filepath.Dir(path), s.plainDir)
	return path, nil
}

// UploadCopy writes a plain copy for a send to a new file of its own, apart from the copy that Plain hands out.
// A media.open during the upload rewrites the Plain copy, so the upload must not read it. The caller removes the file.
func (s *Store) UploadCopy(chat, id string, k Kind, mediaKey []byte) (string, error) {
	if _, _, err := s.names(chat, id); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.plainDir) {
		return "", errNoRuntime
	}
	dir := filepath.Join(s.plainDir, "upload") // no Chat folder: a Chat folder name is 64 hex characters
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "") // mode 0600
	if err != nil {
		return "", err
	}
	f.Close()
	if err := s.Export(chat, id, k, mediaKey, f.Name()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Export writes a plain copy to dst (mode 0600) after the HMAC passed.
// It decrypts into a temp file next to dst and renames it onto dst. A symlink at dst is replaced, not followed.
// On error the temp file goes and an existing dst stays as it was.
func (s *Store) Export(chat, id string, k Kind, mediaKey []byte, dst string) (err error) {
	path, err := s.file(chat, id)
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	iv, cipherKey, macKey := getMediaKeys(mediaKey, k)
	size, err := validate(src, iv, macKey)
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".whisker-save-*") // mode 0600
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(out.Name())
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
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), dst)
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
	if d, err := s.chatDir(chat); err == nil {
		os.RemoveAll(filepath.Join(s.plainDir, d))
		epochAll(s.plainDir)
	}
}

// Remove deletes the stored Media of one Message, for example when a Disappearing Message expires.
func (s *Store) Remove(chat, id string) {
	d, f, err := s.names(chat, id)
	if err != nil {
		return
	}
	os.Remove(filepath.Join(s.dir, d, f))
	plain, _ := filepath.Glob(filepath.Join(s.plainDir, d, f+".*"))
	for _, p := range append(plain, filepath.Join(s.plainDir, d, f)) {
		os.Remove(p)
	}
	epochAll(filepath.Join(s.dir, d), filepath.Join(s.plainDir, d), s.dir)
}

var errMove = errors.New("media move failed")

// MoveChat moves the stored Media of the Chat `from` into the Chat `to`, when the two Chats merge.
// The hashed file names hide the Message ids, so the caller passes the ids of `from`.
// A file whose Message id `to` already has stays as it is there. The plain copies of `from` go.
// It keeps the folder of `from` and returns an error when a move failed, so no file is lost. The error holds no path.
func (s *Store) MoveChat(from, to string, ids []string) error {
	fd, err := s.chatDir(from)
	if err != nil {
		return errMove
	}
	td, err := s.chatDir(to)
	if err != nil {
		return errMove
	}
	s.Release(from)
	failed := false
	for _, id := range ids {
		src, err := s.file(from, id)
		if err != nil {
			failed = true
			continue
		}
		if _, err := os.Stat(src); err != nil || s.Has(to, id) {
			continue // no file, or the target Chat has the id
		}
		dst, _ := s.file(to, id)
		if os.MkdirAll(filepath.Dir(dst), 0o700) != nil || os.Rename(src, dst) != nil {
			failed = true
		}
	}
	epochAll(filepath.Join(s.dir, td), s.dir)
	if failed {
		return errMove
	}
	// The files that stayed go with the folder. So does a file without a Media row: it cannot be opened.
	os.RemoveAll(filepath.Join(s.dir, fd))
	epochAll(filepath.Join(s.dir, td), s.dir)
	return nil
}

// Rename gives the files of the old layout, <Chat>/<id>, their hashed names. A folder whose name is 64 hex characters is already new.
// It is safe to run twice. A failed rename keeps the old file and the old folder.
func (s *Store) Rename() error {
	if len(s.nameKey) == 0 {
		return errNoKey
	}
	dirs, err := os.ReadDir(s.dir)
	if err != nil {
		return nil // no Media
	}
	failed := false
	var hashDirs []string
	for _, d := range dirs {
		if !d.IsDir() || isHash(d.Name()) {
			continue
		}
		old := filepath.Join(s.dir, d.Name())
		files, err := os.ReadDir(old)
		if err != nil {
			failed = true
			continue
		}
		for _, f := range files {
			if strings.HasPrefix(f.Name(), ".put-") { // a temp file of a Put that never ended
				os.Remove(filepath.Join(old, f.Name()))
				continue
			}
			dst, err := s.file(d.Name(), f.Name())
			if err != nil || f.IsDir() || os.MkdirAll(filepath.Dir(dst), 0o700) != nil {
				failed = true
				continue
			}
			if err := os.Rename(filepath.Join(old, f.Name()), dst); err != nil || epoch(dst) != nil {
				failed = true
			}
			hashDirs = append(hashDirs, filepath.Dir(dst))
		}
		os.Remove(old) // fails, and is right to, while a file stays
	}
	epochAll(s.dir)
	for _, d := range hashDirs {
		epochAll(d)
	}
	if failed {
		return errMove
	}
	return nil
}

func isHash(name string) bool {
	b, err := hex.DecodeString(name)
	return err == nil && len(b) == sha256.Size
}

// Wipe deletes every plain copy. Call it at start and exit.
func (s *Store) Wipe() {
	if filepath.IsAbs(s.plainDir) {
		os.RemoveAll(s.plainDir)
	}
}
