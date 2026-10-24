// Package contract is the Daemon's JSON-RPC 2.0 boundary: the only entry point for the Plugin and for tests.
// The wire format is in docs/contract.md.
package contract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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

const (
	errParse          = -32700
	errMethodNotFound = -32601
)

// Run sends `ready`, then answers requests from in until EOF. It writes frames to out and logs to logOut.
// It returns the process exit code.
func Run(in io.Reader, out, logOut io.Writer) int {
	enc := json.NewEncoder(out) // Encode ends each frame with a newline.
	if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "ready", "params": map[string]any{}}); err != nil {
		fmt.Fprintln(logOut, "write failed:", err)
		return ExitError
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(nil, MaxLineBytes+1) // +1 leaves room for the newline.
	for sc.Scan() {
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		var reply map[string]any
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			reply = errorReply(nil, errParse, "parse error")
		} else if len(req.ID) == 0 {
			continue // a notification gets no reply
		} else {
			reply = errorReply(req.ID, errMethodNotFound, "method not found")
		}
		if err := enc.Encode(reply); err != nil {
			fmt.Fprintln(logOut, "write failed:", err)
			return ExitError
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

func errorReply(id json.RawMessage, code int, message string) map[string]any {
	var idValue any // nil encodes as JSON null
	if len(id) > 0 {
		_ = json.Unmarshal(id, &idValue)
	}
	return map[string]any{"jsonrpc": "2.0", "id": idValue, "error": map[string]any{"code": code, "message": message}}
}
