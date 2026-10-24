// Package contract is the Daemon's JSON-RPC 2.0 boundary: the only entry point for the Plugin and for tests.
// The wire format is in docs/contract.md.
package contract

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/keystore"
)

// Exit codes. docs/contract.md mirrors this table.
const (
	ExitOK            = 0
	ExitError         = 1
	ExitOversizedLine = 10
	ExitProfileInUse  = 11
)

// MaxLineBytes is the longest accepted input line, without the newline.
const MaxLineBytes = 1 << 20

const methodKeyringRetry = "keyring.retry"

const (
	errParse          = -32700
	errMethodNotFound = -32601
)

// Profile is what Run needs from the Profile: open the stores.
// Open returns keystore.ErrUnavailable when the keyring does not answer.
type Profile interface{ Open() error }

// Run opens the Profile, sends `ready`, then answers requests from in until EOF.
// While the keyring is unavailable it sends `keyringUnavailable` and waits for `keyring.retry`.
// It writes frames to out and logs to logOut. It returns the process exit code.
func Run(in io.Reader, out, logOut io.Writer, profile Profile) int {
	enc := json.NewEncoder(out) // Encode ends each frame with a newline.
	send := func(frame map[string]any) bool {
		if err := enc.Encode(frame); err != nil {
			fmt.Fprintln(logOut, "write failed:", err)
			return false
		}
		return true
	}
	ready := false
	// open returns an exit code, or -1 to go on.
	open := func() int {
		switch err := profile.Open(); {
		case err == nil:
			ready = true
			if !send(notification("ready")) {
				return ExitError
			}
		case errors.Is(err, keystore.ErrUnavailable):
			fmt.Fprintln(logOut, "keyring unavailable")
			if !send(notification("keyringUnavailable")) {
				return ExitError
			}
		default:
			fmt.Fprintln(logOut, "profile open failed") // the error text could name paths
			return ExitError
		}
		return -1
	}
	if code := open(); code >= 0 {
		return code
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(nil, MaxLineBytes+1) // +1 leaves room for the newline.
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		var reply map[string]any
		retry := false
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			reply = errorReply(nil, errParse, "parse error")
		} else if len(req.ID) == 0 {
			continue // a notification gets no reply
		} else if req.Method == methodKeyringRetry && !ready {
			reply = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}}
			retry = true
		} else {
			reply = errorReply(req.ID, errMethodNotFound, "method not found")
		}
		if !send(reply) {
			return ExitError
		}
		if retry {
			if code := open(); code >= 0 {
				return code
			}
		}
	}
	switch err := sc.Err(); err {
	case nil:
		return ExitOK
	case bufio.ErrTooLong:
		fmt.Fprintf(logOut, "input line too long (over %d bytes)\n", MaxLineBytes)
		return ExitOversizedLine
	default:
		fmt.Fprintln(logOut, "read failed:", err)
		return ExitError
	}
}

func notification(method string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": method, "params": map[string]any{}}
}

func errorReply(id json.RawMessage, code int, message string) map[string]any {
	var idValue any // nil encodes as JSON null
	if len(id) > 0 {
		_ = json.Unmarshal(id, &idValue)
	}
	return map[string]any{"jsonrpc": "2.0", "id": idValue, "error": map[string]any{"code": code, "message": message}}
}
