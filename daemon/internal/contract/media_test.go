package contract_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32)},
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

// startWithVideo starts the Daemon and receives one video without an inline thumbnail, only the link to WhatsApp's.
func startWithVideo(t *testing.T, r *runner) {
	t.Helper()
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "v1", Sender: mediaChat, Time: 5,
		Media: &whatsapp.Media{Kind: media.Video, Mime: "video/mp4", Key: make([]byte, 32), ThumbDirectPath: "/t/v1"},
	}})
	for _, want := range []string{`"none"`, "chatsChanged"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
}

const listVideoChat = `{"jsonrpc":"2.0","id":1,"method":"chat.messages","params":{"chat":"` + mediaChat + `"}}`

// A listed Message without an inline thumbnail gets WhatsApp's separate one, once.
func TestListedMediaGetsItsSeparateThumbnail(t *testing.T) {
	r := newRunner(t)
	r.port.SetThumbnail("thumb", nil)
	startWithVideo(t, r)
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	if f := r.nextFrame(); !strings.Contains(f, "messageChanged") || !strings.Contains(f, `"thumbnail":"dGh1bWI="`) {
		t.Fatalf("frame = %s, want the Message with its thumbnail", f)
	}
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"thumbnail":"dGh1bWI="`) {
		t.Fatalf("frame = %s, want the stored thumbnail in the page", f)
	}
	if n := r.port.ThumbnailDownloads(); n != 1 {
		t.Errorf("thumbnail downloads = %d, want 1", n)
	}
}

// A failed thumbnail download changes nothing and is not tried again in the same run.
func TestFailedThumbnailIsNotRetried(t *testing.T) {
	r := newRunner(t)
	r.port.SetThumbnail("", errors.New("gone"))
	startWithVideo(t, r)
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) || strings.Contains(f, "thumbnail") {
		t.Fatalf("frame = %s, want the page without a thumbnail", f)
	}
	r.waitLog("thumbnail download failed")
	r.send(listVideoChat) // after the failure: no second try
	r.nextFrame()
	if n := r.port.ThumbnailDownloads(); n != 1 {
		t.Errorf("thumbnail downloads = %d, want 1", n)
	}
	if _, _, logs := r.finish(); strings.Contains(logs, "gone") {
		t.Errorf("logs = %q, want the fixed text only", logs)
	}
}

// A thumbnail that failed for another reason than its size keeps its link, so the next run tries again.
func TestFailedThumbnailIsTriedInTheNextRun(t *testing.T) {
	r := newRunner(t)
	r.port.SetThumbnail("", errors.New("offline"))
	startWithVideo(t, r)
	r.send(listVideoChat)
	r.nextFrame()
	r.waitLog("thumbnail download failed")
	r.restart(t, nil)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(listVideoChat)
	r.nextFrame()
	for deadline := time.Now().Add(5 * time.Second); r.currentPort().ThumbnailDownloads() < 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the next run did not fetch the thumbnail again")
		}
	}
}

// The real download fails with a wrapped ErrThumbnailTooLarge: the link is cleared, so the next run does not fetch it.
func TestThumbnailTooLargeErrorClearsTheLink(t *testing.T) {
	r := newRunner(t)
	r.port.SetThumbnail("", fmt.Errorf("last host: %w", whatsapp.ErrThumbnailTooLarge))
	startWithVideo(t, r)
	r.send(listVideoChat)
	r.nextFrame()
	r.waitLog("thumbnail download failed")
	r.restart(t, nil)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(listVideoChat)
	r.nextFrame()
	r.send(listVideoChat)
	r.nextFrame()
	if n := r.currentPort().ThumbnailDownloads(); n != 0 {
		t.Errorf("thumbnail downloads in the new run = %d, want 0", n)
	}
}

// A synced image without any thumbnail downloads when it is listed: its chip shows the file instead.
func TestListedImageWithoutAnyThumbnailDownloads(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: mediaChat, LastActive: 5, Messages: []whatsapp.Message{
		{ID: "i1", Sender: mediaChat, Time: 5, Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32)}},
		{ID: "d1", Sender: mediaChat, Time: 6, Media: &whatsapp.Media{Kind: media.Document, Mime: "application/pdf", Key: make([]byte, 32)}},
	}}}})
	r.nextFrame() // chatsChanged
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	if f := r.nextFrame(); !strings.Contains(f, `"id":"i1"`) || !strings.Contains(f, `"ready"`) {
		t.Fatalf("frame = %s, want the image ready", f)
	}
	if n := r.port.Downloads(); n != 1 {
		t.Errorf("downloads = %d, want 1: the document waits for a click", n)
	}
}

// With automatic download off, a live image and a listed image wait for a click.
func TestAutoDownloadOffKeepsMediaWaiting(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(`{"jsonrpc":"2.0","id":9,"method":"settings.set","params":{"autoDownload":false}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"autoDownload":false`) {
		t.Fatalf("frame = %s, want the setting off", f)
	}
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: mediaChat, LastActive: 5, Messages: []whatsapp.Message{
		{ID: "i1", Sender: mediaChat, Time: 5, Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Key: make([]byte, 32)}},
	}}}})
	r.nextFrame() // chatsChanged
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "i2", Sender: mediaChat, Time: 6, Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Key: make([]byte, 32)},
	}})
	if f := r.nextFrame(); !strings.Contains(f, `"state":"none"`) {
		t.Fatalf("frame = %s, want the live image waiting", f)
	}
	r.nextFrame() // chatsChanged
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	r.send(listVideoChat) // a second reply proves no download frame came in between
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page again", f)
	}
	if n := r.port.Downloads(); n != 0 {
		t.Errorf("downloads = %d, want 0", n)
	}
}

// An image over the size limit waits for a click; one under it downloads.
func TestAutoDownloadLimitKeepsLargeMediaWaiting(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(`{"jsonrpc":"2.0","id":9,"method":"settings.set","params":{"autoDownloadLimit":"1mb"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"autoDownloadLimit":"1mb"`) {
		t.Fatalf("frame = %s, want the limit", f)
	}
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "big", Sender: mediaChat, Time: 6, Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 2 << 20, Key: make([]byte, 32)},
	}})
	if f := r.nextFrame(); !strings.Contains(f, `"state":"none"`) {
		t.Fatalf("frame = %s, want the large image waiting", f)
	}
	r.nextFrame() // chatsChanged
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "small", Sender: mediaChat, Time: 7, Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 1000, Key: make([]byte, 32)},
	}})
	for _, want := range []string{`"downloading"`, "chatsChanged", `"ready"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if n := r.port.Downloads(); n != 1 {
		t.Errorf("downloads = %d, want 1", n)
	}
}

// A hostile page of 20 documents with a separate thumbnail link: the oversized thumbnails are dropped,
// at most three downloads run at once, and a new run does not fetch the links again.
func TestOversizedThumbnailsAreDroppedAndNotRetried(t *testing.T) {
	r := newRunner(t)
	r.port.SetThumbnail(strings.Repeat("x", whatsapp.MaxThumbnailBytes+1), nil)
	r.port.SetThumbnailDelay(20 * time.Millisecond)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	for i := range 20 {
		r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
			ID: fmt.Sprintf("d%d", i), Sender: mediaChat, Time: int64(5 + i),
			Media: &whatsapp.Media{Kind: media.Document, Mime: "application/pdf", Key: make([]byte, 32), ThumbDirectPath: fmt.Sprintf("/t/d%d", i)},
		}})
		for range 2 { // none, chatsChanged
			r.nextFrame()
		}
	}
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	for deadline := time.Now().Add(5 * time.Second); r.port.ThumbnailDownloads() < 20; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("thumbnail downloads = %d, want 20", r.port.ThumbnailDownloads())
		}
	}
	if peak := r.port.ThumbnailPeak(); peak > 3 {
		t.Errorf("peak of concurrent thumbnail downloads = %d, want at most 3", peak)
	}
	for deadline := time.Now().Add(5 * time.Second); strings.Count(r.errOut.String(), "thumbnail download failed") < 20; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("logs = %q, want 20 failed thumbnails", r.errOut.String())
		}
	}
	r.send(listVideoChat)
	if f := r.nextFrame(); !strings.Contains(f, `"messages"`) || strings.Contains(f, "thumbnail") || len(f) > 64<<10 {
		t.Fatalf("frame has %d bytes, want the page without a thumbnail", len(f))
	}
	// A new run: the stored links are gone, so nothing is fetched.
	r.restart(t, nil)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(listVideoChat)
	r.nextFrame()
	r.send(listVideoChat)
	r.nextFrame()
	if n := r.currentPort().ThumbnailDownloads(); n != 0 {
		t.Errorf("thumbnail downloads in the new run = %d, want 0", n)
	}
}
