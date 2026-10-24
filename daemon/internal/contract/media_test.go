package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const mediaChat = "1@s.whatsapp.net"

// startWithImage starts the Daemon, receives one image and waits until it is ready.
func startWithImage(t *testing.T, r *runner) {
	t.Helper()
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "m1", Sender: mediaChat, Time: 5,
		Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Key: make([]byte, 32)},
	}})
	for _, want := range []string{`"downloading"`, "chatsChanged", `"ready"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
}

// Plain copies never outlive the Daemon, and an old run's copies go at start.
func TestPlainCopiesAreWipedAtStartAndExit(t *testing.T) {
	r := newRunner(t)
	stale := filepath.Join(r.runtime, "whisker", "media", "old", "x.jpg")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	startWithImage(t, r)
	if _, err := os.Stat(stale); err == nil {
		t.Error("the plain copy of an earlier run stayed")
	}
	r.send(`{"jsonrpc":"2.0","id":1,"method":"media.open","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"path"`) {
		t.Fatalf("frame = %s, want a path", f)
	}
	code, _, logs := r.finish()
	if code != contract.ExitOK {
		t.Errorf("exit code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(r.runtime, "whisker", "media")); err == nil {
		t.Error("the plain copies stayed after exit")
	}
	if strings.Contains(logs, "whatsapp.net") || strings.Contains(logs, r.runtime) {
		t.Errorf("logs hold a Chat or a path: %q", logs)
	}
}

// A changed byte in the stored file gives an error, no plain copy, and no path in the logs.
func TestChangedStoredMediaIsRejected(t *testing.T) {
	r := newRunner(t)
	startWithImage(t, r)
	file := filepath.Join(r.dir, "media", mediaChat, "m1")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	r.send(`{"jsonrpc":"2.0","id":1,"method":"media.open","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); !strings.Contains(f, "-32009") || !strings.Contains(f, "integrity") {
		t.Fatalf("frame = %s, want the integrity error", f)
	}
	if left, _ := os.ReadDir(filepath.Join(r.runtime, "whisker", "media", mediaChat)); len(left) > 0 {
		t.Error("a plain copy stayed behind")
	}
	_, _, logs := r.finish()
	if strings.Contains(logs, r.runtime) || strings.Contains(logs, "whatsapp.net") {
		t.Errorf("logs hold a Chat or a path: %q", logs)
	}
}
