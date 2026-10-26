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

// fakeClipboard puts a wl-paste on PATH that offers `types` and prints `uris` for text/uri-list.
// An empty script body means no wl-paste at all.
func fakeClipboard(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func uriScript(uris string) string {
	return `if [ "$1" = --list-types ]; then printf 'text/uri-list\ntext/plain\n'; else printf '` + uris + `'; fi`
}

func paste(t *testing.T) (string, string) {
	t.Helper()
	r := newRunner(t)
	r.start()
	r.nextFrame() // ready
	r.send(`{"jsonrpc":"2.0","id":1,"method":"draft.paste","params":{"chat":"a@s.whatsapp.net"}}`)
	f := r.nextFrame()
	_, _, log := r.finish()
	return f, log
}

func TestPasteOfOneCopiedFileGivesItsPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a b.pdf")
	if err := os.WriteFile(file, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeClipboard(t, uriScript("# comment\\r\\nfile://"+strings.ReplaceAll(file, " ", "%%20")+"\\r\\n"))
	f, _ := paste(t)
	if want := `"result":{"chat":"a@s.whatsapp.net","pasted":[` + mustJSON(file) + `]}`; !strings.Contains(f, want) {
		t.Errorf("frame = %s, want %s", f, want)
	}
}

func TestPasteOfTwoCopiedFilesGivesBothInClipboardOrder(t *testing.T) {
	dir := t.TempDir()
	b, a := filepath.Join(dir, "b"), filepath.Join(dir, "a")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fakeClipboard(t, uriScript("file://"+b+"\\nfile://"+a+"\\n"))
	f, _ := paste(t)
	if want := `"pasted":[` + mustJSON(b) + `,` + mustJSON(a) + `]`; !strings.Contains(f, want) {
		t.Errorf("frame = %s, want %s", f, want)
	}
}

func TestPasteCountsAPathListedTwiceOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeClipboard(t, uriScript("file://"+file+"\\nfile://"+file+"\\n"))
	f, _ := paste(t)
	if want := `"pasted":[` + mustJSON(file) + `]`; !strings.Contains(f, want) {
		t.Errorf("frame = %s, want %s", f, want)
	}
}

// attachFiles makes n files and stores them as the Draft of the Chat.
func attachFiles(t *testing.T, r *runner, n int) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, "f"+mustJSON(i))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	setAttachments(t, r, 100, paths)
	return paths
}

func TestPasteOverThirtyAttachmentsIsRefusedButADuplicateIsNot(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	stored := attachFiles(t, r, 30)
	extra := filepath.Join(t.TempDir(), "extra")
	if err := os.WriteFile(extra, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeClipboard(t, uriScript("file://"+extra+"\\n"))
	r.send(`{"jsonrpc":"2.0","id":1,"method":"draft.paste","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"code":-32012`) || !strings.Contains(f, "30 files at most") {
		t.Errorf("frame = %s, want -32012", f)
	}

	fakeClipboard(t, uriScript("file://"+stored[0]+"\\n"))
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.paste","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"pasted":[`+mustJSON(stored[0])+`]`) {
		t.Errorf("frame = %s, want the duplicate to pass", f)
	}

	fakeClipboard(t, imageScript)
	r.send(`{"jsonrpc":"2.0","id":3,"method":"draft.paste","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"code":-32012`) {
		t.Errorf("frame = %s, want -32012 for an image too", f)
	}
	r.finish()
}

func TestPasteRefusesADirectory(t *testing.T) {
	fakeClipboard(t, uriScript("file://"+t.TempDir()+"\\n"))
	if f, _ := paste(t); !strings.Contains(f, "-32013") {
		t.Errorf("frame = %s, want not sendable", f)
	}
}

func TestPasteFallsBackToText(t *testing.T) {
	for name, script := range map[string]string{
		"web link":      uriScript("https://example.com/a.pdf\\n"),
		"text only":     `printf 'text/plain\n'`,
		"empty":         `echo "Nothing is copied" >&2; exit 1`,
		"no wl-paste":   "",
		"hung wl-paste": `exec /usr/bin/sleep 10`,
	} {
		t.Run(name, func(t *testing.T) {
			fakeClipboard(t, script)
			start := time.Now()
			f, log := paste(t)
			if !strings.Contains(f, `"pasted":[]`) {
				t.Errorf("frame = %s, want a text paste", f)
			}
			if time.Since(start) > 4*time.Second {
				t.Error("the paste did not time out")
			}
			if (script == "") != strings.Contains(log, "wl-paste not found") {
				t.Errorf("log = %q", log)
			}
		})
	}
}

const imageScript = `if [ "$1" = --list-types ]; then printf 'text/html\nimage/png\ntext/plain\n'; else printf '\211PNG\r\n\032\n\000\000\000\000IEND\256B\140\202'; fi`

// pastedPath sends draft.paste and returns the one path in its result.
func pastedPath(t *testing.T, r *runner, id int) string {
	t.Helper()
	r.send(`{"jsonrpc":"2.0","id":` + mustJSON(id) + `,"method":"draft.paste","params":{"chat":"1@s.whatsapp.net"}}`)
	var f struct {
		Result struct{ Pasted []string } `json:"result"`
	}
	if err := json.Unmarshal([]byte(r.nextFrame()), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Result.Pasted) != 1 {
		t.Fatalf("pasted = %v, want one path", f.Result.Pasted)
	}
	return f.Result.Pasted[0]
}

func setAttachments(t *testing.T, r *runner, id int, paths []string) {
	t.Helper()
	r.send(`{"jsonrpc":"2.0","id":` + mustJSON(id) + `,"method":"draft.set","params":{"chat":"1@s.whatsapp.net","attachments":` + mustJSON(paths) + `}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"result":{}`) {
		t.Fatalf("frame = %s, want an empty result", f)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestPastedImageLivesUntilTheDraftDropsItOrItIsSent(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged

	first := pastedPath(t, r, 1)
	if filepath.Dir(filepath.Dir(first)) != filepath.Join(r.runtime, "whisker", "pasted") || filepath.Base(first) != "Pasted image.png" {
		t.Fatalf("pasted = %q", first)
	}
	if st, err := os.Stat(first); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("pasted file: %v, %v", st, err)
	}
	if b, _ := os.ReadFile(first); string(b) != "\x89PNG\r\n\x1a\n\x00\x00\x00\x00IEND\xaeB`\x82" {
		t.Errorf("content = %q", b)
	}
	setAttachments(t, r, 2, []string{first})

	second := pastedPath(t, r, 3)
	setAttachments(t, r, 4, []string{second})
	if exists(filepath.Dir(first)) {
		t.Error("a replaced pasted image stays")
	}

	own := filepath.Join(t.TempDir(), "own.png")
	if err := os.WriteFile(own, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	setAttachments(t, r, 5, []string{own})
	setAttachments(t, r, 6, nil)
	if exists(second) {
		t.Error("a removed pasted image stays")
	}
	if !exists(own) {
		t.Error("the Daemon deleted a file of the user")
	}

	third := pastedPath(t, r, 7)
	r.send(`{"jsonrpc":"2.0","id":8,"method":"chat.sendMedia","params":{"chat":"1@s.whatsapp.net","paths":[` + mustJSON(third) + `]}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"state":"pending"`) {
		t.Fatalf("frame = %s, want the send", f)
	}
	if exists(third) {
		t.Error("a sent pasted image stays")
	}
	r.finish()
}

func TestPasteRefusesAnImageOverTheLimit(t *testing.T) {
	fakeClipboard(t, `if [ "$1" = --list-types ]; then printf 'image/png\n'; else exec /usr/bin/head -c 104857601 /dev/zero; fi`)
	if f, _ := paste(t); !strings.Contains(f, "-32013") {
		t.Errorf("frame = %s, want not sendable", f)
	}
}

func TestSlowClipboardDoesNotBlockOtherRequests(t *testing.T) {
	fakeClipboard(t, `/usr/bin/sleep 1; printf 'text/plain\n'`)
	r := newRunner(t)
	r.start()
	r.nextFrame() // ready
	r.send(`{"jsonrpc":"2.0","id":1,"method":"draft.paste","params":{"chat":"a@s.whatsapp.net"}}`)
	r.send(`{"jsonrpc":"2.0","id":2,"method":"settings.get"}`)
	if f := r.nextFrame(); !strings.Contains(f, `"id":2`) {
		t.Errorf("first frame = %s, want the reply to settings.get", f)
	}
	if f := r.nextFrame(); !strings.Contains(f, `"id":1`) || !strings.Contains(f, `"pasted":[]`) {
		t.Errorf("second frame = %s, want the late reply to draft.paste", f)
	}
}

func TestPastedImageIsStoredInTheDraftOfTheReplyChat(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	path := pastedPath(t, r, 1)
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.get","params":{"chat":"1@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"attachments":[`+mustJSON(path)+`]`) {
		t.Errorf("frame = %s, want the pasted image in the stored Draft", f)
	}
	r.finish()
}

func TestPasteOfFilesAndTextAsksTheUser(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(file, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `case "$2" in text/uri-list) printf 'file://` + file + `\r\n';; *) printf 'some words';; esac`
	fakeClipboard(t, `if [ "$1" = --list-types ]; then printf 'text/uri-list\ntext/plain\n'; else `+script+`; fi`)
	if f, _ := paste(t); !strings.Contains(f, `"alsoText":true`) || !strings.Contains(f, mustJSON(file)) {
		t.Errorf("frame = %s, want the file and alsoText", f)
	}
	// The file list as text is no text.
	fakeClipboard(t, uriScript("file://"+file+"\\n"))
	if f, _ := paste(t); strings.Contains(f, "alsoText") {
		t.Errorf("frame = %s, want no alsoText", f)
	}
}

func TestPasteReadsOutputOnlyUpToTheLimit(t *testing.T) {
	fakeClipboard(t, `exec cat /dev/zero`)
	start := time.Now()
	if f, _ := paste(t); !strings.Contains(f, `"pasted":[]`) {
		t.Errorf("frame = %s, want a text paste", f)
	}
	if time.Since(start) > time.Second {
		t.Error("the read ran to the timeout instead of stopping at the limit")
	}
}

func TestSecondPasteWhileOneRunsPastesText(t *testing.T) {
	fakeClipboard(t, `/usr/bin/sleep 1; printf 'text/plain\n'`)
	r := newRunner(t)
	r.start()
	r.nextFrame() // ready
	r.send(`{"jsonrpc":"2.0","id":1,"method":"draft.paste","params":{"chat":"a@s.whatsapp.net"}}`)
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.paste","params":{"chat":"a@s.whatsapp.net"}}`)
	if f := r.nextFrame(); !strings.Contains(f, `"id":2`) || !strings.Contains(f, `"pasted":[]`) {
		t.Errorf("first frame = %s, want the at-once reply to the second paste", f)
	}
	r.finish()
}

func TestDraftSetBeforeThePasteReplyKeepsThePastedImage(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	path := pastedPath(t, r, 1)
	setAttachments(t, r, 2, nil) // a keystroke whose draft.set left before the Plugin saw the reply
	if !exists(path) {
		t.Fatal("draft.set deleted the pasted image before the Plugin knew it")
	}
	setAttachments(t, r, 3, []string{path}) // the Plugin adds it
	setAttachments(t, r, 4, nil)            // the user removes it
	if exists(path) {
		t.Error("a removed pasted image stays")
	}
	r.finish()
}

func TestDraftGetAfterAMissedPasteReplyLetsTheUserRemoveTheImage(t *testing.T) {
	fakeClipboard(t, imageScript)
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	path := pastedPath(t, r, 1) // the Plugin dropped this reply: the user left the Chat
	r.send(`{"jsonrpc":"2.0","id":2,"method":"draft.get","params":{"chat":"1@s.whatsapp.net"}}`)
	r.nextFrame()
	setAttachments(t, r, 3, nil)
	if exists(path) {
		t.Error("a pasted image that the user removed stays")
	}
	r.finish()
}
