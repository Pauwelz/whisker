package contract_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// childOf is a paired child of the photo p1 with Media m.
func childOf(m *whatsapp.Media) whatsapp.MediaReplaced {
	return whatsapp.MediaReplaced{Chat: mediaChat, ID: "p1", Sender: mediaChat, Media: m,
		Child: whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{ID: "c1", Sender: mediaChat, Time: 6, Media: m}}}
}

// hdChild is the paired child of the photo p1: the same photo in HD.
func hdChild() whatsapp.MediaReplaced {
	return childOf(&whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32), DirectPath: "/hd", FileEncSHA256: []byte("hd")})
}

func emitParent(r *runner) {
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{ID: "p1", Sender: mediaChat, Time: 5, Text: "sunset",
		Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32), DirectPath: "/sd", FileEncSHA256: []byte("sd")}}})
}

// page lists the chat page and returns it. Its reply also proves which frames came before it.
func page(t *testing.T, r *runner) string {
	t.Helper()
	r.send(listVideoChat)
	f := r.nextFrame()
	if !strings.Contains(f, `"messages"`) {
		t.Fatalf("frame = %s, want the page", f)
	}
	return f
}

// The HD child of a photo updates the parent line: no second line, no notice. The HD file downloads.
func TestAnHDChildReplacesTheMediaOfTheParentLine(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	emitParent(r)
	for _, want := range []string{`"downloading"`, "chatsChanged", `"ready"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.port.Emit(hdChild())
	for _, want := range []string{`"id":"p1"`, `"ready"`} { // the parent changes, then its HD file is ready
		if f := r.nextFrame(); !strings.Contains(f, "messageChanged") || !strings.Contains(f, want) || !strings.Contains(f, "sunset") {
			t.Fatalf("frame = %s, want the parent with its caption and %s", f, want)
		}
	}
	if f := page(t, r); strings.Contains(f, `"c1"`) {
		t.Errorf("page = %s, want no line for the child", f)
	}
	if n := r.port.Downloads(); n != 2 {
		t.Errorf("downloads = %d, want 2: the photo, then its HD file", n)
	}
}

// The child can arrive while the parent's own file still downloads. That old file must not end up as the HD Media.
func TestAnHDChildDuringTheParentDownloadDownloadsTheHDFile(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.port.SetDownloadDelay(100 * time.Millisecond)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	emitParent(r)
	r.nextFrame() // messageChanged, downloading
	r.nextFrame() // chatsChanged
	r.port.Emit(hdChild())
	r.nextFrame() // messageChanged: the HD Media, downloading
	if f := r.nextFrame(); !strings.Contains(f, `"ready"`) {
		t.Fatalf("frame = %s, want the HD file ready", f)
	}
	if n := r.port.Downloads(); n != 2 {
		t.Errorf("downloads = %d, want 2: the old download and the HD file", n)
	}
}

// A child whose parent is not stored shows as a line of its own: nothing is lost.
func TestAnHDChildWithoutItsParentShowsAsItsOwnLine(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(hdChild())
	if f := r.nextFrame(); !strings.Contains(f, `"id":"c1"`) {
		t.Fatalf("frame = %s, want the child as its own line", f)
	}
	for _, want := range []string{"chatsChanged", `"ready"`} { // the download ends before the test cleans up
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
}

// A motion photo turns the parent line into a video. A video does not download by itself, so it waits for a click.
func TestAMotionPhotoChildTurnsTheParentIntoAVideo(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	emitParent(r)
	for _, want := range []string{`"downloading"`, "chatsChanged", `"ready"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.port.Emit(childOf(&whatsapp.Media{Kind: media.Video, Mime: "video/mp4", Size: 6, Key: make([]byte, 32), DirectPath: "/mp", FileEncSHA256: []byte("mp"), Duration: 2}))
	f := r.nextFrame()
	for _, want := range []string{"messageChanged", `"id":"p1"`, "sunset", `"kind":"video"`, `"duration":2`, `"state":"none"`} {
		if !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if f := page(t, r); strings.Contains(f, `"c1"`) {
		t.Errorf("page = %s, want no line for the child", f)
	}
}

// nextFrames reads frames until each wanted text came, in any order. It fails when a frame matches none of them.
func nextFrames(t *testing.T, r *runner, wants ...string) {
	t.Helper()
	left := append([]string(nil), wants...)
	for len(left) > 0 {
		f := r.nextFrame()
		i := slices.IndexFunc(left, func(w string) bool { return strings.Contains(f, w) })
		if i < 0 {
			t.Fatalf("frame = %s, want one of %q", f, left)
		}
		left = slices.Delete(left, i, i+1)
	}
}

// A child can arrive before its parent, for example when the parent needed a retry. The parent then takes the Media of the child,
// and the line of the child goes.
func TestAParentAfterItsChildTakesTheMediaOfTheChild(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(childOf(&whatsapp.Media{Kind: media.Video, Mime: "video/mp4", Size: 6, Key: make([]byte, 32), DirectPath: "/mp", FileEncSHA256: []byte("mp"), Duration: 2}))
	nextFrames(t, r, `"id":"c1"`, "chatsChanged") // a video waits for a click
	emitParent(r)
	nextFrames(t, r, `"id":"p1"`, "chatsChanged", `"kind":"video"`, `"messageExpired"`, "chatsChanged")
	f := page(t, r)
	if strings.Contains(f, `"c1"`) || !strings.Contains(f, `"kind":"video"`) || !strings.Contains(f, "sunset") {
		t.Errorf("page = %s, want only the parent, with its caption and the video of the child", f)
	}
}

// A child of another author never takes over a Message: its line stays.
func TestAChildOfAnotherAuthorKeepsItsLine(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetDownload("pixels", nil)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	child := childOf(&whatsapp.Media{Kind: media.Video, Mime: "video/mp4", Size: 6, Key: make([]byte, 32), DirectPath: "/mp", FileEncSHA256: []byte("mp")})
	child.FromMe, child.Sender = true, ""
	child.Child.Message.FromMe, child.Child.Message.Sender = true, ""
	r.port.Emit(child)
	nextFrames(t, r, `"id":"c1"`, "chatsChanged")
	emitParent(r)
	nextFrames(t, r, `"id":"p1"`, "chatsChanged", `"ready"`)
	if f := page(t, r); !strings.Contains(f, `"c1"`) || !strings.Contains(f, `"p1"`) {
		t.Errorf("page = %s, want both lines", f)
	}
}
