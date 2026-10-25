package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// pasteTimeout bounds each wl-paste call: a bad clipboard owner can make it hang.
const pasteTimeout = 2 * time.Second

// pastedImageTypes are the image types a paste takes, best first, with the extension that sniffMime knows.
var pastedImageTypes = []struct{ mime, ext string }{{"image/png", ".png"}, {"image/jpeg", ".jpg"}, {"image/webp", ".webp"}}

// pasteResult is what the clipboard read of a draft.paste found. The goroutine fills it and the Daemon loop answers with it.
type pasteResult struct {
	id          json.RawMessage
	chat        string
	files       []string // the copied files, in clipboard order
	image       string   // the path of the image that the read wrote
	tooLarge    bool     // the clipboard image is over maxSendBytes
	notFound    bool     // wl-paste is not installed
	writeFailed bool     // the image could not be written to pastedDir
}

// draftPaste starts the clipboard read for a Ctrl+V in the composer. It gives no reply: pasteDone answers later,
// so a slow clipboard owner stalls no other request.
func (s *session) draftPaste(req *request) outcome {
	id, chat := req.ID, req.Params.Chat
	go func() { s.pasteDone <- readClipboard(id, chat) }()
	return outcome{}
}

// readClipboard runs off the Daemon loop, so it is no session method: it must not touch the session state.
func readClipboard(id json.RawMessage, chat string) pasteResult {
	r := pasteResult{id: id, chat: chat}
	out, err := wlPaste("--list-types")
	if err != nil {
		r.notFound = errors.Is(err, exec.ErrNotFound)
		return r
	}
	types := strings.Fields(string(out))
	r.files = copiedFiles(types)
	i := slices.IndexFunc(pastedImageTypes, func(t struct{ mime, ext string }) bool { return slices.Contains(types, t.mime) })
	if len(r.files) == 0 && i >= 0 {
		var err error
		r.image, err = pasteImage(pastedImageTypes[i].mime, pastedImageTypes[i].ext)
		r.tooLarge = errors.Is(err, errTooLarge)
		r.writeFailed = errors.Is(err, errPasteWrite)
	}
	return r
}

// pasted answers a draft.paste with the files or the image that the read found. An empty `pasted` tells the Plugin to paste text:
// no file or image, no wl-paste, or a timeout. Files win over an image, and an image wins over text.
// Error -32012 means that the stored Attachments plus the new paths would pass maxAttachments.
// The check uses the Draft of now, not the Draft of the request.
func (s *session) pasted(r pasteResult) int {
	if r.writeFailed {
		fmt.Fprintln(s.logOut, "paste write failed")
	}
	if r.notFound && !s.noWlPaste {
		s.noWlPaste = true
		fmt.Fprintln(s.logOut, "wl-paste not found")
	}
	reply := func(paths ...string) int {
		return exitIf(s.send(resultReply(r.id, map[string]any{"chat": r.chat, "pasted": append([]string{}, paths...)})))
	}
	fail := func(code int, msg string) int {
		dropPasted(r.image)
		return exitIf(s.send(errorReply(r.id, code, msg)))
	}
	if !s.ready { // an unlink came while the read ran
		dropPasted(r.image)
		return reply()
	}
	if len(r.files) == 0 && r.image == "" {
		if r.tooLarge {
			return fail(errNotSendable, msgNotSendable)
		}
		return reply() // a failed read or write pastes text
	}
	stored, err := s.chats.Draft(r.chat)
	if err != nil {
		fmt.Fprintln(s.logOut, "draft read failed")
		return fail(errChatStore, msgChatStore)
	}
	if len(r.files) > 0 {
		added := 0
		for _, f := range r.files {
			if !slices.Contains(stored.Attachments, f) {
				added++
			}
		}
		if len(stored.Attachments)+added > maxAttachments {
			return fail(errTooManyFiles, msgTooManyFiles)
		}
		for _, f := range r.files {
			if _, err := statSendable(f); err != nil {
				return fail(errNotSendable, msgNotSendable)
			}
		}
		return reply(r.files...)
	}
	if len(stored.Attachments) >= maxAttachments {
		return fail(errTooManyFiles, msgTooManyFiles)
	}
	return reply(r.image)
}

// copiedFiles gives the paths of the file:// URIs on the clipboard, as a file manager copies them, without duplicates.
func copiedFiles(types []string) []string {
	if !slices.Contains(types, "text/uri-list") {
		return nil
	}
	list, err := wlPaste("--type", "text/uri-list")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(list), "\n") {
		line = strings.TrimSpace(line) // the list ends lines with \r\n
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
			continue // a web link pastes as text
		}
		if !slices.Contains(files, u.Path) { // a path listed twice counts once
			files = append(files, u.Path)
		}
	}
	return files
}

var (
	errTooLarge   = errors.New("too large")
	errPasteWrite = errors.New("paste write failed")
)

// pasteImage writes the clipboard image to its own folder in pastedDir, as "Pasted image" plus the extension.
// The folder keeps the name unique, and the Plugin shows the file name in the Attachment row.
// pastedDir is in memory, so the plain image never reaches the disk. A reboot clears it.
func pasteImage(mime, ext string) (string, error) {
	data, err := wlPaste("--type", mime)
	switch {
	case err != nil:
		return "", err
	case len(data) == 0:
		return "", nil
	case len(data) > maxSendBytes:
		return "", errTooLarge
	}
	root := pastedDir()
	if root == "" {
		return "", nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", errPasteWrite
	}
	dir, err := os.MkdirTemp(root, "") // mode 0700
	if err != nil {
		return "", errPasteWrite
	}
	path := filepath.Join(dir, "Pasted image"+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		os.RemoveAll(dir)
		return "", errPasteWrite
	}
	return path, nil
}

// pastedDir holds the images that a paste wrote. It is empty without XDG_RUNTIME_DIR.
func pastedDir() string {
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		return ""
	}
	return filepath.Join(rt, "whisker", "pasted")
}

// dropPasted deletes an image that a paste wrote, with its folder. Any other path stays: it is the user's file.
// shortcut: no sweep at start, so an image of a crashed Daemon stays until a reboot clears XDG_RUNTIME_DIR.
func dropPasted(path string) {
	if root := pastedDir(); root != "" && path != "" && filepath.Dir(filepath.Dir(path)) == root {
		os.RemoveAll(filepath.Dir(path))
	}
}

// dropAllPasted deletes every image that a paste wrote.
func dropAllPasted() {
	if root := pastedDir(); root != "" {
		os.RemoveAll(root)
	}
}

// wlPaste runs wl-paste. wl-clipboard is optional: without it, a paste is text only.
func wlPaste(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pasteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	cmd.WaitDelay = 100 * time.Millisecond // a child that keeps stdout open must not outlast the timeout
	return cmd.Output()
}
