package contract

import (
	"context"
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

// draftPaste reads the clipboard for a Ctrl+V in the composer. `pasted` lists the paths of the copied files in clipboard order,
// or the path of a pasted image that the Daemon wrote. Files win over an image, and an image wins over text.
// An empty `pasted` tells the Plugin to paste text: no file or image, no wl-paste, or a timeout.
// Error -32012 means that the stored Attachments plus the new paths would pass maxAttachments.
// shortcut: the read runs on the Daemon loop for up to 3 × pasteTimeout, move it off the loop when that stall shows.
func (s *session) draftPaste(req *request) outcome {
	pasted := func(paths ...string) outcome {
		return outcome{reply: resultReply(req.ID, map[string]any{"chat": req.Params.Chat, "pasted": append([]string{}, paths...)})}
	}
	out, err := s.wlPaste("--list-types")
	if err != nil {
		return pasted()
	}
	types := strings.Fields(string(out))
	files := s.copiedFiles(types)
	i := slices.IndexFunc(pastedImageTypes, func(t struct{ mime, ext string }) bool { return slices.Contains(types, t.mime) })
	if len(files) == 0 && i < 0 {
		return pasted()
	}
	stored, err := s.chats.Draft(req.Params.Chat)
	if err != nil {
		fmt.Fprintln(s.logOut, "draft read failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	if len(files) > 0 {
		added := 0
		for _, f := range files {
			if !slices.Contains(stored.Attachments, f) {
				added++
			}
		}
		if len(stored.Attachments)+added > maxAttachments {
			return outcome{reply: errorReply(req.ID, errTooManyFiles, msgTooManyFiles)}
		}
		for _, f := range files {
			if _, err := statSendable(f); err != nil {
				return outcome{reply: errorReply(req.ID, errNotSendable, msgNotSendable)}
			}
		}
		return pasted(files...)
	}
	if len(stored.Attachments) >= maxAttachments {
		return outcome{reply: errorReply(req.ID, errTooManyFiles, msgTooManyFiles)}
	}
	t := pastedImageTypes[i]
	path, err := s.pasteImage(t.mime, t.ext)
	if errors.Is(err, errTooLarge) {
		return outcome{reply: errorReply(req.ID, errNotSendable, msgNotSendable)}
	}
	if path == "" { // a failed read or write pastes text
		return pasted()
	}
	return pasted(path)
}

// copiedFiles gives the paths of the file:// URIs on the clipboard, as a file manager copies them, without duplicates.
func (s *session) copiedFiles(types []string) []string {
	if !slices.Contains(types, "text/uri-list") {
		return nil
	}
	list, err := s.wlPaste("--type", "text/uri-list")
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

var errTooLarge = errors.New("too large")

// pasteImage writes the clipboard image to its own folder in pastedDir, as "Pasted image" plus the extension.
// The folder keeps the name unique, and the Plugin shows the file name in the Attachment row.
// pastedDir is in memory, so the plain image never reaches the disk. A reboot clears it.
func (s *session) pasteImage(mime, ext string) (string, error) {
	data, err := s.wlPaste("--type", mime)
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
		fmt.Fprintln(s.logOut, "paste write failed")
		return "", err
	}
	dir, err := os.MkdirTemp(root, "") // mode 0700
	if err != nil {
		fmt.Fprintln(s.logOut, "paste write failed")
		return "", err
	}
	path := filepath.Join(dir, "Pasted image"+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		fmt.Fprintln(s.logOut, "paste write failed")
		os.RemoveAll(dir)
		return "", err
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
func (s *session) wlPaste(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pasteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	cmd.WaitDelay = 100 * time.Millisecond // a child that keeps stdout open must not outlast the timeout
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) && !s.noWlPaste {
		s.noWlPaste = true
		fmt.Fprintln(s.logOut, "wl-paste not found")
	}
	return out, err
}
