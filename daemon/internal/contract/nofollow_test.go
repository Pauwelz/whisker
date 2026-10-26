package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func refusedWith(t *testing.T, r *runner, path string) {
	t.Helper()
	sendMedia(r, 50, []string{path}, "")
	if f := r.nextFrame(); !strings.Contains(f, `"code":-32013`) {
		t.Fatalf("frame = %s, want -32013", f)
	}
	if up := r.port.Uploads(); len(up) != 0 {
		t.Fatalf("uploads = %v, want none", up)
	}
}

func TestSendRefusesAnAttachmentSwappedForASymlink(t *testing.T) {
	r := linkedRunner(t)
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	write(t, secret, "secret")
	path := filepath.Join(dir, "a.txt")
	write(t, path, "plain")
	setAttachments(t, r, 1, []string{path})
	os.Remove(path)
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	refusedWith(t, r, path)
	r.finish()
}

func TestSendRefusesAnAttachmentWhoseFolderBecameASymlink(t *testing.T) {
	r := linkedRunner(t)
	dir, other := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "sub", "a.pdf")
	write(t, path, "mine")
	write(t, filepath.Join(other, "a.pdf"), "theirs")
	setAttachments(t, r, 1, []string{path})
	if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	refusedWith(t, r, path)
	r.finish()
}

func TestDraftSetStoresTheRealPathOfASymlink(t *testing.T) {
	r := linkedRunner(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	write(t, target, "x")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(target)
	setAttachments(t, r, 1, []string{link})
	d := getDraft(t, r, 2)
	if got := d.Result.Draft.Attachments; len(got) != 1 || got[0] != want {
		t.Errorf("attachments = %v, want [%s]", got, want)
	}
	if len(d.Result.Unsendable) != 0 {
		t.Errorf("unsendable = %v", d.Result.Unsendable)
	}
	r.finish()
}

func TestAFileInASymlinkedFolderAtAttachTimeSends(t *testing.T) {
	r := linkedRunner(t)
	real := t.TempDir()
	write(t, filepath.Join(real, "a.pdf"), "%PDF-1.4 hi")
	link := filepath.Join(t.TempDir(), "Downloads")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	setAttachments(t, r, 1, []string{filepath.Join(link, "a.pdf")})
	reply, upload := uploadAfterConnect(t, r, getDraft(t, r, 2).Result.Draft.Attachments[0])
	if !strings.Contains(reply, `"sent"`) || !strings.Contains(upload, "|a.pdf|") {
		t.Errorf("reply = %s, upload = %s", reply, upload)
	}
	r.finish()
}

func TestADeletedAttachmentStillSavesAndIsUnsendable(t *testing.T) {
	r := linkedRunner(t)
	path := filepath.Join(t.TempDir(), "gone.txt")
	write(t, path, "x")
	setAttachments(t, r, 1, []string{path})
	os.Remove(path)
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.set","params":{"chat":"1@s.whatsapp.net","text":"hi","attachments":` + mustJSON([]string{path}) + `}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"result":{}`) {
		t.Fatalf("frame = %s", f)
	}
	d := getDraft(t, r, 3)
	b, _ := json.Marshal(d.Result.Unsendable)
	if string(b) != mustJSON([]string{path}) {
		t.Errorf("unsendable = %s", b)
	}
	r.finish()
}
