package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Limits for the output of each wl-paste call. wl-paste is killed past its limit.
const (
	maxTypesBytes = 64 << 10 // the type list
	maxTextBytes  = 64 << 10 // the text, and the file list
)

// pastedImageTypes are the image types a paste takes, best first, with the extension that sniffMime knows.
var pastedImageTypes = []struct{ mime, ext string }{{"image/png", ".png"}, {"image/jpeg", ".jpg"}, {"image/webp", ".webp"}}

// pasteResult is what the clipboard read of a draft.paste found. The goroutine fills it and the Daemon loop answers with it.
type pasteResult struct {
	id          json.RawMessage
	chat        string
	files       []string // the copied files, in clipboard order
	text        bool     // the clipboard also has text that is no copy of the file list
	image       string   // the path of the image that the read wrote
	tooLarge    bool     // the clipboard image is over maxSendBytes
	notFound    bool     // wl-paste is not installed
	writeFailed bool     // the image could not be written to pastedDir
}

// draftPaste starts the clipboard read for a Ctrl+V in the composer. It gives no reply: pasteDone answers later,
// so a slow clipboard owner stalls no other request.
func (s *session) draftPaste(req *request) outcome {
	id, chat := req.ID, req.Params.Chat
	if s.pasteBusy { // one clipboard read at a time: a second Ctrl+V pastes text
		return outcome{reply: resultReply(id, map[string]any{"chat": chat, "pasted": []string{}})}
	}
	s.pasteBusy = true
	go func() { s.pasteDone <- readClipboard(id, chat) }()
	return outcome{}
}

// readClipboard runs off the Daemon loop, so it is no session method: it must not touch the session state.
func readClipboard(id json.RawMessage, chat string) pasteResult {
	r := pasteResult{id: id, chat: chat}
	out, err := wlPaste(maxTypesBytes, "--list-types")
	if err != nil {
		r.notFound = errors.Is(err, exec.ErrNotFound)
		return r
	}
	types := strings.Fields(string(out))
	r.files = copiedFiles(types)
	r.text = len(r.files) > 0 && clipboardText(types, r.files)
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
	s.pasteBusy = false
	if r.writeFailed {
		fmt.Fprintln(s.logOut, "paste write failed")
	}
	if r.notFound && !s.noWlPaste {
		s.noWlPaste = true
		fmt.Fprintln(s.logOut, "wl-paste not found")
	}
	reply := func(paths ...string) int {
		result := map[string]any{"chat": r.chat, "pasted": append([]string{}, paths...)}
		if r.text && len(paths) > 0 && r.image == "" {
			result["alsoText"] = true // the Plugin asks which one the user wants
		}
		return exitIf(s.send(resultReply(r.id, result)))
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
		for i, f := range r.files {
			real, err := resolveSendable(f)
			if err != nil {
				return fail(errNotSendable, msgNotSendable)
			}
			r.files[i] = real
		}
		added := 0
		for _, f := range r.files {
			if !slices.Contains(stored.Attachments, f) {
				added++
			}
		}
		if len(stored.Attachments)+added > maxAttachments {
			return fail(errTooManyFiles, msgTooManyFiles)
		}
		return reply(r.files...)
	}
	if len(stored.Attachments) >= maxAttachments {
		return fail(errTooManyFiles, msgTooManyFiles)
	}
	if stored.Edit != "" { // an Edit started while the read ran: files never combine with it
		dropPasted(r.image)
		return reply()
	}
	// Store the image at once, so a switch of Chat cannot orphan the file. The Plugin stores the same list again.
	stored.Quote = ""
	stored.Attachments = append(slices.Clone(stored.Attachments), r.image)
	if err := s.chats.SetDraft(r.chat, stored); err != nil {
		fmt.Fprintln(s.logOut, "draft write failed")
		return fail(errChatStore, msgChatStore)
	}
	if s.pastedPending == nil {
		s.pastedPending = map[string]bool{}
	}
	s.pastedPending[r.image] = true
	return reply(r.image)
}

// copiedFiles gives the paths of the file:// URIs on the clipboard, as a file manager copies them, without duplicates.
func copiedFiles(types []string) []string {
	if !slices.Contains(types, "text/uri-list") {
		return nil
	}
	list, err := wlPaste(maxTextBytes, "--type", "text/uri-list")
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

// clipboardText tells that the clipboard has text besides the copied files. A file manager often offers the file list as
// text too (paths or file:// lines): that is no text.
func clipboardText(types, files []string) bool {
	i := slices.IndexFunc(types, func(t string) bool { return strings.HasPrefix(t, "text/plain") || t == "UTF8_STRING" })
	if i < 0 {
		return false
	}
	data, err := wlPaste(maxTextBytes, "--type", types[i])
	if errors.Is(err, errTooLarge) {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "file://") && !slices.Contains(files, line) {
			return true
		}
	}
	return false
}

var (
	errTooLarge   = errors.New("too large")
	errPasteWrite = errors.New("paste write failed")
)

// pasteImage writes the clipboard image to its own folder in pastedDir, as "Pasted image" plus the extension.
// The folder keeps the name unique, and the Plugin shows the file name in the Attachment row.
// pastedDir is in memory, so the plain image never reaches the disk. A reboot clears it.
func pasteImage(mime, ext string) (string, error) {
	data, err := wlPaste(maxSendBytes, "--type", mime)
	switch {
	case err != nil:
		return "", err // errTooLarge too
	case len(data) == 0:
		return "", nil
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
	if real, err := filepath.EvalSymlinks(rt); err == nil {
		rt = real // the Daemon refuses a path with a symlink, so the paste folder must be real
	}
	return filepath.Join(rt, "whisker", "pasted")
}

// dropPasted deletes an image that a paste wrote, with its folder. Any other path stays: it is the user's file.
// shortcut: no sweep at start (the Media wipe covers whisker/media only), so an image of a crashed Daemon stays until a reboot clears XDG_RUNTIME_DIR.
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

// wlPaste runs wl-paste and reads at most `limit` bytes of its output. It kills wl-paste past the limit and gives errTooLarge.
// wl-clipboard is optional: without it, a paste is text only.
func wlPaste(limit int64, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pasteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	cmd.WaitDelay = 100 * time.Millisecond // a child that keeps stdout open must not outlast the timeout
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	context.AfterFunc(ctx, func() { out.Close() }) // unblocks the read when wl-paste hangs
	data, readErr := io.ReadAll(io.LimitReader(out, limit+1))
	if int64(len(data)) > limit {
		cancel()
		cmd.Wait()
		return nil, errTooLarge
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return data, readErr
}
