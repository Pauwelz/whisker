package media_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

var key = bytes.Repeat([]byte{7}, 32)

func newStore(t *testing.T) (*media.Store, string, string) {
	t.Helper()
	enc, plain := filepath.Join(t.TempDir(), "media"), filepath.Join(t.TempDir(), "plain")
	s := media.New(enc, plain)
	s.SetNameKey(media.NameKey(key))
	return s, enc, plain
}

// stored is the one file in the Chat folder of the encrypted store.
func stored1(t *testing.T, enc string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(enc, "*", "*"))
	if len(m) != 1 {
		t.Fatalf("stored files = %v, want one", m)
	}
	return m[0]
}

func TestStoredMediaIsEncryptedAndComesBackPlain(t *testing.T) {
	s, enc, _ := newStore(t)
	text := []byte(strings.Repeat("secret picture ", 5000))
	if err := s.Put("c@s.whatsapp.net", "m1", media.Image, key, bytes.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(stored1(t, enc))
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
		file := stored1(t, enc)
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
		if left, _ := filepath.Glob(filepath.Join(plain, "*", "*")); len(left) > 0 {
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
	s.MoveChat("lid", "phone", []string{"a", "dup"})
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
	dirs, _ := filepath.Glob(filepath.Join(enc, "*"))
	for _, d := range dirs {
		if err := os.Chmod(d, 0o500); err != nil { // a read-only folder refuses the rename
			t.Fatal(err)
		}
		defer os.Chmod(d, 0o700)
	}
	if os.Getuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	err := s.MoveChat("lid", "phone", []string{"a"})
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

const jid, msgID = "31612345678@s.whatsapp.net", "3EB0ABCDEF"

func TestNamesHideTheChatTheIdAndTheTime(t *testing.T) {
	s, enc, plain := newStore(t)
	if err := s.Put(jid, msgID, media.Image, key, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plain(jid, msgID, ".jpg", media.Image, key); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{enc, plain} {
		n := 0
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			rel, _ := filepath.Rel(root, p)
			for _, secret := range []string{"31612345678", msgID, "whatsapp"} {
				if strings.Contains(rel, secret) {
					t.Errorf("path %q holds %q", rel, secret)
				}
			}
			if !d.IsDir() {
				n++
			}
			if fi, _ := d.Info(); !fi.ModTime().Equal(time.Unix(0, 0)) {
				t.Errorf("mtime of %q = %v, want the epoch", rel, fi.ModTime())
			}
			return nil
		})
		if n != 1 {
			t.Errorf("%s holds %d files, want 1", root, n)
		}
	}
}

func TestEveryMethodFindsTheFileUnderItsNewName(t *testing.T) {
	s, enc, plain := newStore(t)
	if err := s.Put(jid, msgID, media.Image, key, strings.NewReader("pic")); err != nil {
		t.Fatal(err)
	}
	if !s.Has(jid, msgID) || s.Has(jid, "other") {
		t.Error("Has")
	}
	if p, err := s.Plain(jid, msgID, ".jpg", media.Image, key); err != nil || filepath.Ext(p) != ".jpg" {
		t.Errorf("Plain = %q, %v", p, err)
	}
	up, err := s.UploadCopy(jid, msgID, media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(up); string(got) != "pic" {
		t.Errorf("UploadCopy = %q", got)
	}
	os.Remove(up) // the caller removes the upload copy
	dst := filepath.Join(t.TempDir(), "out")
	if err := s.Export(jid, msgID, media.Image, key, dst); err != nil {
		t.Errorf("Export: %v", err)
	}
	s.Release(jid)
	if m, _ := filepath.Glob(filepath.Join(plain, "*", "*")); len(m) != 0 {
		t.Errorf("Release left %v", m)
	}
	s.Plain(jid, msgID, ".jpg", media.Image, key)
	s.Remove(jid, msgID)
	if s.Has(jid, msgID) {
		t.Error("Remove kept the file")
	}
	if m, _ := filepath.Glob(filepath.Join(plain, "*", "*")); len(m) != 0 {
		t.Errorf("Remove left the plain copy %v", m)
	}
	if m, _ := filepath.Glob(filepath.Join(enc, "*", "*")); len(m) != 0 {
		t.Errorf("Remove left %v", m)
	}
}

func TestMoveChatOpensTheMediaUnderTheNewChat(t *testing.T) {
	s, _, _ := newStore(t)
	lid := "1234567890@lid"
	if err := s.Put(lid, "a", media.Image, key, strings.NewReader("lid file")); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveChat(lid, jid, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	path, err := s.Plain(jid, "a", "", media.Image, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "lid file" {
		t.Errorf("moved file = %q", got)
	}
}

func TestStoreWithoutTheKeyWipesAndRefusesTheRest(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(filepath.Join(plain, "x"), 0o700)
	s := media.New(filepath.Join(t.TempDir(), "media"), plain)
	if err := s.Put(jid, msgID, media.Image, key, strings.NewReader("x")); err == nil {
		t.Error("Put without the key must fail")
	}
	if _, err := s.Plain(jid, msgID, "", media.Image, key); err == nil {
		t.Error("Plain without the key must fail")
	}
	if s.Has(jid, msgID) || s.MoveChat(jid, "b", nil) == nil || s.Rename() == nil {
		t.Error("a store without the key must refuse")
	}
	s.Wipe()
	if _, err := os.Stat(plain); err == nil {
		t.Error("Wipe did not clear the plain folder")
	}
}

func TestOtherKeysGiveOtherNames(t *testing.T) {
	names := map[string]bool{}
	for _, k := range [][]byte{key, bytes.Repeat([]byte{8}, 32)} {
		enc := filepath.Join(t.TempDir(), "media")
		s := media.New(enc, "")
		s.SetNameKey(media.NameKey(k))
		if err := s.Put(jid, msgID, media.Image, key, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
		names[stored1(t, enc)[len(enc):]] = true
	}
	if len(names) != 2 {
		t.Error("two keys gave the same name")
	}
}

func TestEmptyNamesAreRefused(t *testing.T) {
	s, enc, _ := newStore(t)
	if err := s.Put(jid, msgID, media.Image, key, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if s.Has(jid, "") || s.Has("", msgID) {
		t.Error("Has accepted an empty name")
	}
	if s.Put(jid, "", media.Image, key, strings.NewReader("x")) == nil || s.Put("", msgID, media.Image, key, strings.NewReader("x")) == nil {
		t.Error("Put accepted an empty name")
	}
	if s.Export(jid, "", media.Image, key, filepath.Join(t.TempDir(), "o")) == nil {
		t.Error("Export accepted an empty id")
	}
	s.Remove(jid, "")
	if m, _ := filepath.Glob(filepath.Join(enc, "*", "*")); len(m) != 1 {
		t.Errorf("files = %v, want the one file to stay", m)
	}
}

func TestExportReplacesASymlinkAndKeepsTheFileOnFailure(t *testing.T) {
	s, _, _ := newStore(t)
	if err := s.Put("c", "m", media.Document, key, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep"), 0o644)
	dst := filepath.Join(dir, "out")
	if err := os.Symlink(victim, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.Export("c", "m", media.Document, key, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("Export followed the symlink: victim = %q", got)
	}
	if st, _ := os.Lstat(dst); st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0o600 {
		t.Errorf("dst mode = %v", st.Mode())
	}
	if got, _ := os.ReadFile(dst); string(got) != "data" {
		t.Errorf("dst = %q", got)
	}
	os.WriteFile(victim, []byte("keep"), 0o644)
	if s.Export("c", "missing", media.Document, key, victim) == nil {
		t.Fatal("export of a missing Media passed")
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("a failed Export touched the file: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("Export left %d entries", len(entries))
	}
}
