package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// linkedRunner starts a linked Daemon with the Chat 1@s.whatsapp.net.
func linkedRunner(t *testing.T) *runner {
	t.Helper()
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	return r
}

func sendMedia(r *runner, id int, paths []string, caption string) {
	r.send(`{"jsonrpc":"2.0","id":` + mustJSON(id) + `,"method":"chat.sendMedia","params":{"chat":"1@s.whatsapp.net","paths":` + mustJSON(paths) + `,"caption":` + mustJSON(caption) + `}}`)
}

type draftReply struct {
	Result struct {
		Draft struct {
			Text        string
			Attachments []string
		}
		Unsendable []string
	}
}

func getDraft(t *testing.T, r *runner, id int) draftReply {
	t.Helper()
	r.send(`{"jsonrpc":"2.0","id":` + mustJSON(id) + `,"method":"draft.get","params":{"chat":"1@s.whatsapp.net"}}`)
	var d draftReply
	if err := json.Unmarshal([]byte(r.nextFrame()), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSendMediaStoresOneMessagePerPathWithTheCaptionOnTheFirst(t *testing.T) {
	r := linkedRunner(t)
	paths := attachFiles(t, r, 3)
	sendMedia(r, 1, paths, "look")
	var f struct {
		Result struct{ Sent []struct{ ID, Text string } }
	}
	if err := json.Unmarshal([]byte(r.nextFrame()), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Result.Sent) != 3 || f.Result.Sent[0].Text != "look" || f.Result.Sent[1].Text != "" || f.Result.Sent[2].Text != "" {
		t.Fatalf("sent = %+v, want 3 Messages and the caption on the first", f.Result.Sent)
	}
	if d := getDraft(t, r, 2); len(d.Result.Draft.Attachments) != 0 || d.Result.Draft.Text != "" {
		t.Errorf("Draft = %+v, want it cleared", d.Result.Draft)
	}
	r.finish()
}

func TestSendMediaWithOneBadPathStoresNothingAndKeepsTheDraft(t *testing.T) {
	r := linkedRunner(t)
	paths := attachFiles(t, r, 3)
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths[2]); err != nil {
		t.Fatal(err)
	}
	sendMedia(r, 1, paths, "look")
	want := `"error":{"code":-32013,"data":{"paths":` + mustJSON([]string{paths[0], paths[2]}) + `},"message":"file not sendable"}`
	if f := r.nextFrame(); !strings.Contains(f, want) {
		t.Fatalf("frame = %s, want %s", f, want)
	}
	d := getDraft(t, r, 2)
	if len(d.Result.Draft.Attachments) != 3 {
		t.Errorf("Draft = %+v, want it kept", d.Result.Draft)
	}
	if strings.Join(d.Result.Unsendable, ",") != paths[0]+","+paths[2] {
		t.Errorf("unsendable = %v", d.Result.Unsendable)
	}
	r.send(`{"jsonrpc":"2.0","id":3,"method":"chat.messages","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"messages":[]`) {
		t.Errorf("frame = %s, want no stored Message", f)
	}
	r.finish()
}

func TestSendMediaRefusesNoPathsAndMoreThanThirty(t *testing.T) {
	r := linkedRunner(t)
	sendMedia(r, 1, nil, "")
	if f := r.nextFrame(); !strings.Contains(f, "-32006") {
		t.Errorf("frame = %s, want invalid message", f)
	}
	sendMedia(r, 2, make([]string, 31), "")
	if f := r.nextFrame(); !strings.Contains(f, "-32006") {
		t.Errorf("frame = %s, want invalid message", f)
	}
	r.finish()
}

func TestDraftSetRemovesOnlyTheDroppedPastedImage(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := linkedRunner(t)
	keep, drop := pastedPath(t, r, 1), pastedPath(t, r, 2)
	own := filepath.Join(t.TempDir(), "own.png")
	if err := os.WriteFile(own, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAttachments(t, r, 3, []string{keep, drop, own})
	setAttachments(t, r, 4, []string{keep})
	if !exists(keep) || exists(drop) || !exists(own) {
		t.Errorf("keep %v, drop %v, own %v; want true, false, true", exists(keep), exists(drop), exists(own))
	}
	r.finish()
}

func TestDraftSetChecksOnlyNewPathsAndRefusesFilesWithAQuote(t *testing.T) {
	r := linkedRunner(t)
	paths := attachFiles(t, r, 2)
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	setAttachments(t, r, 1, paths) // a stored file is gone: a keystroke still saves
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.set","params":{"chat":"1@s.whatsapp.net","attachments":` + mustJSON(append(paths, "relative.png")) + `}}`)
	if f := r.nextFrame(); !strings.Contains(f, "-32006") {
		t.Errorf("frame = %s, want a new bad path refused", f)
	}
	if d := getDraft(t, r, 3); len(d.Result.Unsendable) != 1 || d.Result.Unsendable[0] != paths[0] || len(d.Result.Draft.Attachments) != 2 {
		t.Errorf("reply = %+v, want the gone file kept and listed", d.Result)
	}
	r.finish()
}

func TestSendMediaWithAnUnreadableSecondFileStoresNothingAndKeepsTheFirstPastedImage(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := linkedRunner(t)
	pasted := pastedPath(t, r, 1)
	unreadable := filepath.Join(t.TempDir(), "locked")
	if err := os.WriteFile(unreadable, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAttachments(t, r, 2, []string{pasted, unreadable})
	if err := os.Chmod(unreadable, 0); err != nil { // unreadable after the attach: the open at send time fails
		t.Fatal(err)
	}
	sendMedia(r, 3, []string{pasted, unreadable}, "look")
	if f := r.nextFrame(); !strings.Contains(f, "-32013") {
		t.Fatalf("frame = %s, want an unsendable file", f)
	}
	if !exists(pasted) {
		t.Error("the pasted image of the first path is gone")
	}
	if d := getDraft(t, r, 4); len(d.Result.Draft.Attachments) != 2 {
		t.Errorf("Draft = %+v, want it kept", d.Result.Draft)
	}
	r.send(`{"jsonrpc":"2.0","id":5,"method":"chat.messages","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"messages":[]`) {
		t.Errorf("frame = %s, want no stored Message", f)
	}
	r.finish()
}

// gpsJPEG is a tiny JPEG with an APP1 Exif segment that holds a GPS marker.
func gpsJPEG() []byte {
	exif := "Exif\x00\x00GPSLatitude"
	app1 := append([]byte{0xFF, 0xE1, 0, byte(len(exif) + 2)}, exif...)
	b := append([]byte{0xFF, 0xD8}, app1...)
	return append(b, 0xFF, 0xDA, 0, 2, 1, 2, 3, 0xFF, 0xD9)
}

func writeFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// uploadAfterConnect connects the Daemon, sends one file and returns the reply and the upload.
func uploadAfterConnect(t *testing.T, r *runner, path string) (reply string, upload string) {
	t.Helper()
	r.port.Emit(whatsapp.Connected{})
	r.nextFrame() // connected
	sendMedia(r, 1, []string{path}, "")
	reply = r.nextFrame()
	for len(r.port.Uploads()) == 0 {
		time.Sleep(time.Millisecond)
	}
	return reply, r.port.Uploads()[0]
}

func TestSendMediaStripsTheGPSMarkerFromAnImage(t *testing.T) {
	r := linkedRunner(t)
	in := gpsJPEG()
	reply, upload := uploadAfterConnect(t, r, writeFile(t, "a.jpg", in))
	content := upload[strings.LastIndex(upload, "|")+1:]
	if strings.Contains(content, "GPS") || strings.Contains(content, "Exif") {
		t.Errorf("upload still holds metadata: %q", content)
	}
	if want := `"size":` + mustJSON(len(content)); !strings.Contains(reply, want) {
		t.Errorf("reply = %s, want %s", reply, want)
	}
	r.finish()
}

func TestSendMediaOfABrokenImageIsRefusedAndStoresNothing(t *testing.T) {
	r := linkedRunner(t)
	path := writeFile(t, "bad.jpg", []byte("not a jpeg"))
	sendMedia(r, 1, []string{path}, "")
	want := `"error":{"code":-32013,"data":{"paths":` + mustJSON([]string{path}) + `},"message":"file not sendable"}`
	if f := r.nextFrame(); !strings.Contains(f, want) {
		t.Fatalf("frame = %s, want %s", f, want)
	}
	r.send(`{"jsonrpc":"2.0","id":3,"method":"chat.messages","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"messages":[]`) {
		t.Errorf("frame = %s, want no stored Message", f)
	}
	r.finish()
}

func TestSendMediaKeepsADocumentByteExact(t *testing.T) {
	r := linkedRunner(t)
	in := gpsJPEG()
	_, upload := uploadAfterConnect(t, r, writeFile(t, "a.bin", in))
	if !strings.Contains(upload, "|document|") || !strings.HasSuffix(upload, string(in)) {
		t.Errorf("upload = %q, want the unchanged bytes as a document", upload)
	}
	r.finish()
}
