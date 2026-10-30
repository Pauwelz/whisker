package contract_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// expiredImage receives an image whose link is dead and waits until the Daemon asks the phone for it again.
func expiredImage(t *testing.T) *runner {
	t.Helper()
	r := startedRunner(t)
	r.port.SetDownload("pixels", nil)
	r.port.SetExpired("/old")
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "m1", Sender: mediaChat, Time: 5,
		Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32), DirectPath: "/old"},
	}})
	for _, want := range []string{`"downloading"`, "chatsChanged"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); len(r.port.MediaRetryRequests()) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the Daemon did not ask the phone for the Media")
		}
	}
	return r
}

// A link that expired is recovered through the phone: the new link downloads and the Media is ready.
func TestExpiredMediaIsRecoveredThroughThePhone(t *testing.T) {
	r := expiredImage(t)
	if got := r.port.MediaRetryRequests(); len(got) != 1 || got[0] != mediaChat+"|"+mediaChat+"|m1|false" {
		t.Errorf("requests = %v", got)
	}
	r.port.Emit(whatsapp.MediaRetried{Chat: mediaChat, ID: "m1", DirectPath: "/new"})
	if f := r.nextFrame(); !strings.Contains(f, `"ready"`) {
		t.Fatalf("frame = %s, want ready", f)
	}
	if n := r.port.Downloads(); n != 2 {
		t.Errorf("downloads = %d, want 2", n)
	}
	r.send(`{"jsonrpc":"2.0","id":1,"method":"media.open","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"path"`) {
		t.Fatalf("frame = %s, want a path", f)
	}
}

// Media that the phone no longer holds ends in a final state: the Plugin stops offering Retry and the Daemon refuses it.
func TestMediaNotOnThePhoneIsFinal(t *testing.T) {
	r := expiredImage(t)
	r.port.Emit(whatsapp.MediaRetried{Chat: mediaChat, ID: "m1", NotOnPhone: true})
	if f := r.nextFrame(); !strings.Contains(f, `"gone"`) {
		t.Fatalf("frame = %s, want gone", f)
	}
	r.send(`{"jsonrpc":"2.0","id":2,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); !strings.Contains(f, "-32008") {
		t.Fatalf("frame = %s, want error -32008", f)
	}
	if n := r.port.Downloads(); n != 1 {
		t.Errorf("downloads = %d, want 1", n)
	}
}

// A phone answer for a Message that nobody asked about changes nothing.
func TestUnaskedMediaRetryAnswerIsIgnored(t *testing.T) {
	r := startedRunner(t)
	r.port.Emit(whatsapp.MediaRetried{Chat: mediaChat, ID: "nope", NotOnPhone: true})
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{ID: "t1", Sender: mediaChat, Time: 6, Text: "hi"}})
	if f := r.nextFrame(); strings.Contains(f, "gone") {
		t.Fatalf("frame = %s", f)
	}
}

// A failure that is not an expired link behaves as before: failed, and no request to the phone.
func TestOtherMediaFailureAsksNothing(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload("", errors.New("boom"))
	emitMedia(r, "m1", media.Image, 6)
	for _, want := range []string{`"downloading"`, "chatsChanged", `"failed"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if got := r.port.MediaRetryRequests(); len(got) != 0 {
		t.Errorf("requests = %v, want none", got)
	}
}

// A link that stays dead after the phone answered fails once. It does not loop.
func TestRecoveredLinkThatExpiresAgainFails(t *testing.T) {
	r := expiredImage(t)
	r.port.SetExpired("/old", "/new")
	r.port.Emit(whatsapp.MediaRetried{Chat: mediaChat, ID: "m1", DirectPath: "/new"})
	if f := r.nextFrame(); !strings.Contains(f, `"failed"`) {
		t.Fatalf("frame = %s, want failed", f)
	}
	if got := r.port.MediaRetryRequests(); len(got) != 1 {
		t.Errorf("requests = %v, want one", got)
	}
}

// A phone that never answers ends the wait: the Media fails (Retry shows again), the request is dropped, and a late answer changes nothing.
func TestUnansweredMediaRetryFails(t *testing.T) {
	r := expiredImage(t)
	r.expiry.Fire() // the sweep timer, set first
	r.expiry.Fire() // the wait for the phone
	if f := r.nextFrame(); !strings.Contains(f, `"failed"`) {
		t.Fatalf("frame = %s, want failed", f)
	}
	if got := r.port.ForgottenMediaRetries(); len(got) != 1 || got[0] != mediaChat+"|m1" {
		t.Errorf("forgotten = %v, want the request of m1", got)
	}
	r.port.Emit(whatsapp.MediaRetried{Chat: mediaChat, ID: "m1", DirectPath: "/new"})
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{ID: "t1", Sender: mediaChat, Time: 6, Text: "hi"}})
	if f := r.nextFrame(); strings.Contains(f, `"ready"`) {
		t.Fatalf("frame = %s, a late answer must not download", f)
	}
	if n := r.port.Downloads(); n != 1 {
		t.Errorf("downloads = %d, want 1", n)
	}
	r.send(`{"jsonrpc":"2.0","id":3,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); strings.Contains(f, "-32008") {
		t.Fatalf("frame = %s, a failed Media can start again", f)
	}
}

// A request to the phone that cannot be sent ends the Media as failed, at once.
func TestMediaRetryThatCannotBeSentFails(t *testing.T) {
	r := startedRunner(t)
	r.port.SetDownload("pixels", nil)
	r.port.SetExpired("/old")
	r.port.SetMediaRetryError(errors.New("boom"))
	r.port.Emit(whatsapp.NewMessage{Chat: mediaChat, Message: whatsapp.Message{
		ID: "m1", Sender: mediaChat, Time: 5,
		Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 6, Key: make([]byte, 32), DirectPath: "/old"},
	}})
	for _, want := range []string{`"downloading"`, "chatsChanged", `"failed"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.send(`{"jsonrpc":"2.0","id":3,"method":"media.download","params":{"chat":"` + mediaChat + `","id":"m1"}}`)
	if f := r.nextFrame(); strings.Contains(f, "-32008") {
		t.Fatalf("frame = %s, a failed Media can start again", f)
	}
}
