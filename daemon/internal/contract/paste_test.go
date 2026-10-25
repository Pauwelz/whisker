package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if want := `"result":{"chat":"a@s.whatsapp.net","pasted":` + mustJSON(file) + `}`; !strings.Contains(f, want) {
		t.Errorf("frame = %s, want %s", f, want)
	}
}

func TestPasteRefusesTwoFiles(t *testing.T) {
	dir := t.TempDir()
	fakeClipboard(t, uriScript("file://"+dir+"/a\\nfile://"+dir+"/b\\n"))
	if f, _ := paste(t); !strings.Contains(f, "-32012") {
		t.Errorf("frame = %s, want paste one file at a time", f)
	}
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
			if !strings.Contains(f, `"pasted":""`) {
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
