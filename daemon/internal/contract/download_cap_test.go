package contract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	kib = 1 << 10
	mib = 1 << 20
)

// noStrayFiles checks that a failed download left no stored file and no temp file.
func noStrayFiles(t *testing.T, r *runner) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(r.dir, "media", mediaChat, "m1")); err == nil {
		t.Error("a stored file stayed after a failed download")
	}
	left, _ := filepath.Glob(filepath.Join(r.runtime, "whisker", "media", ".ingest-*"))
	if len(left) > 0 {
		t.Errorf("temp files stayed: %v", left)
	}
}

func startedRunner(t *testing.T) *runner {
	t.Helper()
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	return r
}

func emitMedia(r *runner, id string, kind media.Kind, size int64) {
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: id, Sender: mediaChat, Time: 5,
		Media: &whatsapp.Media{Kind: kind, Mime: "application/octet-stream", Size: size, Key: make([]byte, 32)},
	}})
}

// A sender that declares 1 KiB and sends 10 MiB: the automatic download stops, fails, and keeps nothing.
func TestAutoDownloadFailsWhenMoreBytesArriveThanDeclared(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload(strings.Repeat("x", 10*mib), nil)
	emitMedia(r, "m1", media.Image, kib)
	for _, want := range []string{`"downloading"`, "chatsChanged", `"failed"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	noStrayFiles(t, r)
}

// The same lie on a click: a document of 1 KiB that sends 10 MiB fails too.
func TestManualDownloadFailsWhenMoreBytesArriveThanDeclared(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload(strings.Repeat("x", 10*mib), nil)
	emitMedia(r, "m1", media.Document, kib)
	r.nextFrame() // none
	r.nextFrame() // chatsChanged
	r.send(`{"jsonrpc":"2.0","id":2,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	r.waitLog("media download failed")
	noStrayFiles(t, r)
	r.send(listVideoChat)
	for range 4 {
		if f := r.nextFrame(); strings.Contains(f, `"messages"`) {
			if !strings.Contains(f, `"failed"`) {
				t.Errorf("frame = %s, want the Media failed", f)
			}
			return
		}
	}
	t.Fatal("no page")
}

// An honest file under the limit still downloads by itself.
func TestHonestDownloadStillWorks(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload(strings.Repeat("x", 4*mib), nil)
	emitMedia(r, "m1", media.Image, 4*mib)
	for _, want := range []string{`"downloading"`, "chatsChanged", `"ready"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
}

// An unknown size waits for a click under a numeric limit, and the click downloads it.
func TestUnknownSizeWaitsForAClick(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload("pixels", nil)
	emitMedia(r, "m1", media.Image, 0)
	if f := r.nextFrame(); !strings.Contains(f, `"state":"none"`) {
		t.Fatalf("frame = %s, want the image waiting", f)
	}
	r.nextFrame() // chatsChanged
	if n := r.port.Downloads(); n != 0 {
		t.Fatalf("downloads = %d, want 0", n)
	}
	r.send(`{"jsonrpc":"2.0","id":2,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	for range 4 {
		if f := r.nextFrame(); strings.Contains(f, `"ready"`) {
			return
		}
	}
	t.Fatal("the click did not download the image")
}

// Five images are due at once; at most three Download calls run at the same time.
func TestFullDownloadsRunThreeAtATime(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload("pixels", nil)
	r.port.SetDownloadDelay(30 * time.Millisecond)
	for i := range 5 {
		emitMedia(r, fmt.Sprintf("m%d", i), media.Image, 6)
	}
	for deadline := time.Now().Add(5 * time.Second); r.port.Downloads() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("downloads = %d, want 5", r.port.Downloads())
		}
	}
	if peak := r.port.DownloadPeak(); peak > 3 || peak < 2 {
		t.Errorf("peak of concurrent downloads = %d, want 3 at most (and some overlap)", peak)
	}
}
