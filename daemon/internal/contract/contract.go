// Package contract is the Daemon's JSON-RPC 2.0 boundary: the only entry point for the Plugin and for tests.
// The wire format is in docs/contract.md.
package contract

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"rsc.io/qr"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
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
	methodKeyringRetry = "keyring.retry"
	methodLinkPairCode = "link.pairCode"
	methodLinkRetry    = "link.retry"
	methodChatsList    = "chats.list"
	methodSettingsGet  = "settings.get"
	methodSettingsSet  = "settings.set"
	methodLogout       = "profile.logout"
)

// Page size of chats.list: the default, and the most a request can ask for.
const (
	defaultChatsLimit = 50
	maxChatsLimit     = 200
)

const (
	errParse          = -32700
	errMethodNotFound = -32601
	errInvalidPhone   = -32001
	errPairCode       = -32002
	errChatStore      = -32003
	errSetting        = -32004
	errLogout         = -32005
)

// qrScale is the pixel size of one QR module.
const qrScale = 8

// Profile is what Run needs from the Profile: open the stores, the chat store, and delete the Profile.
// Open returns keystore.ErrUnavailable when the keyring does not answer. Chat is valid after Open.
type Profile interface {
	Open() error
	Chat() *sql.DB
	Delete() error
}

// NewPort makes the WhatsApp port for the Profile that Open just opened.
type NewPort func() (whatsapp.Port, error)

// Run opens the Profile, sends `ready`, starts linking or connecting, then serves requests and WhatsApp events until EOF.
// While the keyring is unavailable it sends `keyringUnavailable` and waits for `keyring.retry`.
// Profile Settings live in prefs, outside the Profile, so Delete keeps them.
// It writes frames to out and logs to logOut. It returns the process exit code.
func Run(in io.Reader, out, logOut io.Writer, profile Profile, prefs *settings.Store, newPort NewPort) int {
	enc := json.NewEncoder(out) // Encode ends each frame with a newline.
	send := func(frame map[string]any) bool {
		if err := enc.Encode(frame); err != nil {
			fmt.Fprintln(logOut, "write failed:", err)
			return false
		}
		return true
	}
	ready := false
	var port whatsapp.Port
	var events <-chan whatsapp.Event // nil (never ready) until a port exists
	var chats *chatstore.Store       // nil until the Profile is open

	// connect connects the port. An unlinked Profile starts linking, and a failure ends it with `linkFailed`.
	connect := func() bool {
		if err := port.Connect(); err != nil {
			fmt.Fprintln(logOut, "connect failed") // the error text could name hosts
			if !port.Linked() {
				return send(notification("linkFailed", map[string]any{"reason": whatsapp.ReasonConnectFailed}))
			}
		}
		return true
	}
	// startPort runs after `ready`: a linked Profile says so, then the port connects.
	startPort := func() bool {
		if port.Linked() && !send(notification("linked", nil)) {
			return false
		}
		return connect()
	}
	// open returns an exit code, or -1 to go on.
	open := func() int {
		switch err := profile.Open(); {
		case err == nil:
			// The port exists before `ready`, so a test or the Plugin can rely on it from then on.
			var portErr error
			if port, portErr = newPort(); portErr != nil {
				fmt.Fprintln(logOut, "whatsapp client failed")
				return ExitError
			}
			events = port.Events()
			chats = chatstore.New(profile.Chat(), time.Now)
			ready = true
			if !send(notification("ready", nil)) || !startPort() {
				return ExitError
			}
		case errors.Is(err, keystore.ErrUnavailable):
			fmt.Fprintln(logOut, "keyring unavailable")
			if !send(notification("keyringUnavailable", nil)) {
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

	// unlink runs the delete-the-Profile routine, then links again as on a fresh Profile.
	// `by` is "user" for Log out. It is empty when the phone removed the Companion Device.
	unlink := func(by string) int {
		port.Disconnect()
		ready, port, events, chats = false, nil, nil, nil
		if err := profile.Delete(); err != nil {
			fmt.Fprintln(logOut, "profile delete failed")
			return ExitError
		}
		params := map[string]any{}
		if by != "" {
			params["by"] = by
		}
		if !send(notification("unlinked", params)) {
			return ExitError
		}
		return open()
	}

	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(nil, MaxLineBytes+1) // +1 leaves room for the newline.
		for sc.Scan() {
			lines <- append([]byte(nil), sc.Bytes()...)
		}
		scanErr <- sc.Err()
		close(lines)
	}()

	for {
		select {
		case ev := <-events:
			if code := handleEvent(ev, logOut, send, chats, func() int { return unlink("") }); code >= 0 {
				return code
			}
		case line, ok := <-lines:
			if !ok {
				return exitCode(<-scanErr, logOut)
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					Phone  string `json:"phone"`
					Offset int    `json:"offset"`
					Limit  int    `json:"limit"`
					Query  string `json:"query"`
					settings.Update
				} `json:"params"`
			}
			var reply map[string]any
			retryKeyring, retryLink, logout := false, false, false
			linking := ready && !port.Linked()
			if err := json.Unmarshal(line, &req); err != nil {
				reply = errorReply(nil, errParse, "parse error")
			} else if len(req.ID) == 0 {
				continue // a notification gets no reply
			} else if req.Method == methodKeyringRetry && !ready {
				reply = resultReply(req.ID, map[string]any{})
				retryKeyring = true
			} else if req.Method == methodLinkRetry && linking {
				reply = resultReply(req.ID, map[string]any{})
				retryLink = true
			} else if req.Method == methodLinkPairCode && linking {
				reply = pairCodeReply(port, req.ID, req.Params.Phone, logOut)
			} else if req.Method == methodChatsList && ready {
				reply = chatsListReply(chats, req.ID, req.Params.Offset, req.Params.Limit, req.Params.Query, logOut)
			} else if req.Method == methodSettingsGet && ready {
				reply = settingsReply(req.ID, logOut, func() (settings.Settings, error) { return prefs.Get() })
			} else if req.Method == methodSettingsSet && ready {
				reply = settingsReply(req.ID, logOut, func() (settings.Settings, error) { return prefs.Set(req.Params.Update) })
			} else if req.Method == methodLogout && ready && port.Linked() {
				if err := port.Logout(); err != nil {
					fmt.Fprintln(logOut, "logout failed") // the error text could name hosts
					reply = errorReply(req.ID, errLogout, "log out failed")
				} else {
					reply = resultReply(req.ID, map[string]any{})
					logout = true
				}
			} else {
				reply = errorReply(req.ID, errMethodNotFound, "method not found")
			}
			if !send(reply) {
				return ExitError
			}
			if retryKeyring {
				if code := open(); code >= 0 {
					return code
				}
			}
			if retryLink && !connect() {
				return ExitError
			}
			if logout {
				if code := unlink("user"); code >= 0 {
					return code
				}
			}
		}
	}
}

// handleEvent turns a WhatsApp event into frames. unlink runs the delete-the-Profile routine.
// It returns an exit code, or -1 to go on.
func handleEvent(ev whatsapp.Event, logOut io.Writer, send func(map[string]any) bool, chats *chatstore.Store, unlink func() int) int {
	ok := true
	switch ev := ev.(type) {
	case whatsapp.QRCode:
		uri, err := qrDataURI(ev.Code)
		if err != nil {
			fmt.Fprintln(logOut, "qr encode failed")
			ok = send(notification("linkFailed", map[string]any{"reason": whatsapp.ReasonUnexpected}))
		} else {
			ok = send(notification("linkQr", map[string]any{"qr": uri}))
		}
	case whatsapp.PairSuccess:
		ok = send(notification("linked", nil))
	case whatsapp.LinkFailed:
		ok = send(notification("linkFailed", map[string]any{"reason": ev.Reason}))
	case whatsapp.LoggedOut:
		return unlink()
	case whatsapp.HistorySync, whatsapp.PushName, whatsapp.ContactName, whatsapp.Pin, whatsapp.Mute, whatsapp.ReadMarker:
		if err := chats.Apply(ev); err != nil {
			fmt.Fprintln(logOut, "chat store write failed") // the error text could hold names
		} else {
			ok = send(notification("chatsChanged", nil))
		}
	}
	if !ok {
		return ExitError
	}
	return -1
}

func pairCodeReply(port whatsapp.Port, id json.RawMessage, phone string, logOut io.Writer) map[string]any {
	// shortcut: PairCode blocks the Daemon loop (up to 30 s), upgrade when another request must run meanwhile.
	code, err := port.PairCode(phone)
	switch {
	case errors.Is(err, whatsapp.ErrInvalidPhone):
		return errorReply(id, errInvalidPhone, "invalid phone number")
	case err != nil:
		fmt.Fprintln(logOut, "pair code failed") // the error text could hold the phone number
		return errorReply(id, errPairCode, "pair code failed")
	}
	return resultReply(id, map[string]any{"code": code})
}

func chatsListReply(chats *chatstore.Store, id json.RawMessage, offset, limit int, query string, logOut io.Writer) map[string]any {
	offset = max(offset, 0)
	if limit <= 0 {
		limit = defaultChatsLimit
	}
	page, err := chats.List(offset, min(limit, maxChatsLimit), query)
	if err != nil {
		fmt.Fprintln(logOut, "chat list failed")
		return errorReply(id, errChatStore, "chat store failed")
	}
	return resultReply(id, map[string]any{"chats": page.Chats, "more": page.More, "offset": offset})
}

func settingsReply(id json.RawMessage, logOut io.Writer, apply func() (settings.Settings, error)) map[string]any {
	cur, err := apply()
	switch {
	case errors.Is(err, settings.ErrInvalid):
		return errorReply(id, errSetting, "invalid setting")
	case err != nil:
		fmt.Fprintln(logOut, "settings failed") // the error text could name paths
		return errorReply(id, errSetting, "settings failed")
	}
	return resultReply(id, map[string]any{"readReceipts": cur.ReadReceipts, "sendOnline": cur.SendOnline, "sendTyping": cur.SendTyping, "notifications": cur.Notifications})
}

// qrDataURI renders a WhatsApp QR code string as a PNG data: URI.
func qrDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	code.Scale = qrScale
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}

func exitCode(err error, logOut io.Writer) int {
	switch err {
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

func resultReply(id json.RawMessage, result map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func notification(method string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	return map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
}

func errorReply(id json.RawMessage, code int, message string) map[string]any {
	var idValue any // nil encodes as JSON null
	if len(id) > 0 {
		_ = json.Unmarshal(id, &idValue)
	}
	return map[string]any{"jsonrpc": "2.0", "id": idValue, "error": map[string]any{"code": code, "message": message}}
}
