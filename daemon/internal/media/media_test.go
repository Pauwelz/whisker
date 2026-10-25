package media_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

var key = bytes.Repeat([]byte{7}, 32)

func newStore(t *testing.T) (*media.Store, string, string) {
	t.Helper()
	enc, plain := filepath.Join(t.TempDir(), "media"), filepath.Join(t.TempDir(), "plain")
	return media.New(enc, plain), enc, plain
}

func TestStoredMediaIsEncryptedAndComesBackPlain(t *testing.T) {
	s, enc, _ := newStore(t)
	text := []byte(strings.Repeat("secret picture ", 5000))
	if err := s.Put("c@s.whatsapp.net", "m1", media.Image, key, bytes.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(enc, "c@s.whatsapp.net", "m1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("secret picture")) {
		t.Fatal("the stored file holds plain text")
	}
	path, err := s.Plain("c@s.whatsapp.net", "m1", ".jpg", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, text) {
		t.Error("the plain copy differs from the original")
	}
	if filepath.Ext(path) != ".jpg" {
		t.Errorf("path = %s, want the extension", path)
	}
}

// The Media integrity test: a changed byte anywhere makes the copy fail, and no plain copy stays.
func TestChangedByteFailsTheIntegrityCheck(t *testing.T) {
	text := []byte(strings.Repeat("x", 100))
	for _, at := range []int{0, 50, 111, 112, 121} { // 112 bytes of ciphertext, then a 10-byte MAC
		s, enc, plain := newStore(t)
		if err := s.Put("c", "m", media.Audio, key, bytes.NewReader(text)); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(enc, "c", "m")
		data, _ := os.ReadFile(file)
		if len(data) != 122 {
			t.Fatalf("stored size = %d, want 122", len(data))
		}
		data[at] ^= 1
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Plain("c", "m", "", media.Audio, key); !errors.Is(err, media.ErrIntegrity) {
			t.Errorf("byte %d: err = %v, want ErrIntegrity", at, err)
		}
		if left, _ := os.ReadDir(filepath.Join(plain, "c")); len(left) > 0 {
			t.Errorf("byte %d: a plain copy stayed behind", at)
		}
	}
}

func TestWrongKindFailsTheIntegrityCheck(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.Put("c", "m", media.Image, key, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plain("c", "m", "", media.Video, key); !errors.Is(err, media.ErrIntegrity) {
		t.Errorf("err = %v, want ErrIntegrity", err)
	}
}

func TestReleaseWipeAndExport(t *testing.T) {
	s, _, plain := newStore(t)
	for _, c := range []string{"c1", "c2"} {
		if err := s.Put(c, "m", media.Document, key, strings.NewReader(c)); err != nil {
			t.Fatal(err)
		}
	}
	p1, _ := s.Plain("c1", "m", ".pdf", media.Document, key)
	p2, _ := s.Plain("c2", "m", ".pdf", media.Document, key)
	s.Release("c1")
	if _, err := os.Stat(p1); err == nil {
		t.Error("Release left the copy of the closed Chat")
	}
	if _, err := os.Stat(p2); err != nil {
		t.Error("Release removed the copy of another Chat")
	}
	dst := filepath.Join(t.TempDir(), "out.pdf")
	if err := s.Export("c2", "m", media.Document, key, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "c2" {
		t.Errorf("export = %q", got)
	}
	s.Wipe()
	if entries, _ := os.ReadDir(plain); len(entries) != 0 {
		t.Errorf("Wipe left %d entries", len(entries))
	}
}

func TestIngestEncryptsWhatTheFetchWrites(t *testing.T) {
	s, _, plain := newStore(t)
	err := s.Ingest("c", "m", media.Video, key, func(f *os.File) error { _, err := f.WriteString("movie"); return err })
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(plain); len(entries) != 0 {
		t.Error("Ingest left its temp file")
	}
	path, err := s.Plain("c", "m", "", media.Video, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "movie" {
		t.Errorf("got %q", got)
	}
	failed := s.Ingest("c", "m2", media.Video, key, func(*os.File) error { return errors.New("net") })
	if failed == nil || s.Has("c", "m2") {
		t.Error("a failed fetch must store nothing")
	}
}

func TestMoveChatKeepsTheFilesOfTheTargetChat(t *testing.T) {
	s, _, _ := newStore(t)
	for _, c := range []struct{ chat, id, text string }{{"lid", "a", "A"}, {"lid", "dup", "from lid"}, {"phone", "dup", "from phone"}} {
		if err := s.Put(c.chat, c.id, media.Image, key, strings.NewReader(c.text)); err != nil {
			t.Fatal(err)
		}
	}
	s.MoveChat("lid", "phone")
	if s.Has("lid", "a") || s.Has("lid", "dup") || !s.Has("phone", "a") {
		t.Error("the files did not move")
	}
	path, err := s.Plain("phone", "dup", "", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "from phone" {
		t.Errorf("dup = %q; the target Chat file must stay", got)
	}
}

func TestMoveChatKeepsTheFolderWhenAMoveFails(t *testing.T) {
	s, enc, _ := newStore(t)
	for _, c := range []struct{ chat, id string }{{"lid", "a"}, {"phone", "b"}} {
		if err := s.Put(c.chat, c.id, media.Image, key, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(enc, "phone")
	if err := os.Chmod(target, 0o500); err != nil { // a read-only folder refuses the rename
		t.Fatal(err)
	}
	defer os.Chmod(target, 0o700)
	if os.Getuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	err := s.MoveChat("lid", "phone")
	if err == nil || strings.Contains(err.Error(), enc) {
		t.Errorf("MoveChat error = %v; want a failure without a path", err)
	}
	if !s.Has("lid", "a") {
		t.Error("the file was lost after a failed move")
	}
}

func TestUnsafeNamesAreRejected(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.Put("../x", "m", media.Image, key, strings.NewReader("a")); err == nil {
		t.Error("a path in the Chat id must fail")
	}
}

// A media.open during an upload rewrites the Plain copy. The upload copy must stay whole, and its removal must keep the Plain copy.
func TestUploadCopyIsApartFromThePlainCopy(t *testing.T) {
	s, _, _ := newStore(t)
	text := []byte(strings.Repeat("picture ", 5000))
	if err := s.Put("c@s.whatsapp.net", "m", media.Image, key, bytes.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	upload, err := s.UploadCopy("c@s.whatsapp.net", "m", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.Plain("c@s.whatsapp.net", "m", ".png", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	if upload == plain {
		t.Fatal("the upload copy is the Plain copy")
	}
	if got, _ := os.ReadFile(upload); !bytes.Equal(got, text) {
		t.Error("the upload copy differs from the original")
	}
	os.Remove(upload)
	if _, err := os.Stat(plain); err != nil {
		t.Error("removing the upload copy removed the Plain copy")
	}
	s.Release("c@s.whatsapp.net")
	again, err := s.UploadCopy("c@s.whatsapp.net", "m", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	if again == upload {
		t.Error("two upload copies share a name")
	}
}
