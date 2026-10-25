package contract

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// pasteTimeout bounds each wl-paste call: a bad clipboard owner can make it hang.
const pasteTimeout = 2 * time.Second

// draftPaste reads the clipboard for a Ctrl+V in the composer. `pasted` is the path of the one copied file.
// An empty `pasted` tells the Plugin to paste text: no file, no wl-paste, or a timeout.
// shortcut: the read runs on the Daemon loop for up to 2 × pasteTimeout, move it off the loop when that stall shows.
func (s *session) draftPaste(req *request) outcome {
	files := s.copiedFiles()
	if len(files) > 1 {
		return outcome{reply: errorReply(req.ID, errPasteOneFile, msgPasteOneFile)}
	}
	pasted := ""
	if len(files) == 1 {
		if _, err := statSendable(files[0]); err != nil {
			return outcome{reply: errorReply(req.ID, errNotSendable, msgNotSendable)}
		}
		pasted = files[0]
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": req.Params.Chat, "pasted": pasted})}
}

// copiedFiles gives the paths of the file:// URIs on the clipboard, as a file manager copies them.
func (s *session) copiedFiles() []string {
	types, err := s.wlPaste("--list-types")
	if err != nil || !slices.Contains(strings.Fields(types), "text/uri-list") {
		return nil
	}
	list, err := s.wlPaste("--type", "text/uri-list")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line) // the list ends lines with \r\n
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
			continue // a web link pastes as text
		}
		files = append(files, u.Path)
	}
	return files
}

// wlPaste runs wl-paste. wl-clipboard is optional: without it, a paste is text only.
func (s *session) wlPaste(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pasteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	cmd.WaitDelay = 100 * time.Millisecond // a child that keeps stdout open must not outlast the timeout
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) && !s.noWlPaste {
		s.noWlPaste = true
		fmt.Fprintln(s.logOut, "wl-paste not found")
	}
	return string(out), err
}
