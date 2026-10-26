package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

// A stored blob with a valid HMAC and a block that does not decrypt to valid padding: Export fails after its temp file exists.
func TestFailedExportLeavesTheFileAndNoTemp(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "enc"), filepath.Join(dir, "plain"))
	key := make([]byte, 32)
	s.SetFileKey(FileKey(key))
	s.SetNameKey(NameKey(key))
	if err := os.MkdirAll(filepath.Dir(must(s.file("c", "m"))), 0o700); err != nil {
		t.Fatal(err)
	}
	iv, _, macKey, _ := s.mediaKeys("m", Document)
	ct := make([]byte, 16)
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ct)
	if err := os.WriteFile(must(s.file("c", "m")), append(ct, h.Sum(nil)[:macLength]...), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	os.WriteFile(out, []byte("keep"), 0o644)
	if err := s.Export("c", "m", Document, out); err == nil {
		t.Fatal("export of a bad block passed")
	}
	if got, _ := os.ReadFile(out); string(got) != "keep" {
		t.Errorf("a failed Export changed the file: %q", got)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".whisker-save-*")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}
