// Package contract is the Daemon's JSON-RPC 2.0 boundary: the only entry point for the Plugin and for tests.
// The wire format is in docs/contract.md.
package contract

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"rsc.io/qr"

	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/media"
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

// Page size of chats.list and chat.messages: the default, and the most a request can ask for.
const (
	defaultChatsLimit = 50
	maxChatsLimit     = 200
)

// maxTextRunes is the WhatsApp limit for the text of a Message.
const maxTextRunes = 65536

// fitsMessage tells that text is within the WhatsApp limit.
func fitsMessage(text string) bool { return utf8.RuneCountInString(text) <= maxTextRunes }

// validText tells that text can be sent or become an edit: not blank, and within the limit.
func validText(text string) bool { return strings.TrimSpace(text) != "" && fitsMessage(text) }

// whatsappQuote is the part of a quoted Message that goes to WhatsApp.
func whatsappQuote(id, sender, text string) *whatsapp.Quote {
	return &whatsapp.Quote{ID: id, Sender: sender, Text: text}
}

const (
	errParse          = -32700
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInvalidPhone   = -32001
	errPairCode       = -32002
	errChatStore      = -32003
	errSetting        = -32004
	errMessage        = -32006
	errNoWhatsApp     = -32007
	errMedia          = -32008
	errMediaFailed    = -32009
	errInvalidChat    = -32010
	errPinLimit       = -32011
	errTooManyFiles   = -32012
	errNotSendable    = -32013
)

// The message of each application error. A code that has two messages has two constants.
const (
	msgParse          = "parse error"
	msgMethodNotFound = "method not found"
	msgInvalidParams  = "invalid params"
	msgInvalidPhone   = "invalid phone number"
	msgPairCode       = "pair code failed"
	msgChatStore      = "chat store failed"
	msgInvalidSetting = "invalid setting"
	msgSettingsFailed = "settings failed"
	msgInvalidMessage = "invalid message"
	msgNoWhatsApp     = "could not reach WhatsApp"
	msgInvalidMedia   = "invalid media"
	msgMediaIntegrity = "media failed the integrity check"
	msgMediaCopy      = "media copy failed"
	msgInvalidChat    = "invalid chat"
	msgPinLimit       = "pin limit"
	msgTooManyFiles   = "30 files at most"
	msgNotSendable    = "file not sendable"
)

// qrScale is the pixel size of one QR module.
const qrScale = 8

// KeyringPlain tells whether the keyring that holds the store key has no password. The Daemon asks on each start, before `ready`.
// main sets it; tests replace it.
var KeyringPlain = func() bool { return false }

// Profile is what Run needs from the Profile: open the stores, the chat store, delete the Profile,
// and tell whether Open set an unreadable Profile aside.
// Open returns keystore.ErrUnavailable when the keyring does not answer. Chat is valid after Open.
type Profile interface {
	Open() error
	Chat() *sql.DB
	Media() *media.Store
	Delete() error
	MovedAside() bool
}

// NewPort makes the WhatsApp port for the Profile that Open just opened.
type NewPort func() (whatsapp.Port, error)

// Run opens the Profile, sends `ready`, starts linking or connecting, then serves requests and WhatsApp events until EOF.
// While the keyring is unavailable it sends `keyringUnavailable` and waits for `keyring.retry`.
// The desktop shows the notifications.
// Profile Settings live in prefs, outside the Profile, so Delete keeps them.
// It writes frames to out and logs to logOut. It returns the process exit code.
func Run(in io.Reader, out, logOut io.Writer, profile Profile, prefs *settings.Store, newPort NewPort, desktop desktop.Notifier) int {
	enc := json.NewEncoder(out) // Encode ends each frame with a newline.
	s := &session{profile: profile, prefs: prefs, newPort: newPort, desktop: desktop, logOut: logOut, pasteDone: make(chan pasteResult, 64)}
	s.send = func(frame map[string]any) bool {
		if err := enc.Encode(frame); err != nil {
			fmt.Fprintln(logOut, "write failed:", err)
			return false
		}
		return true
	}
	defer s.wipeMedia()
	if code := s.open(); code != keepGoing {
		return code
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
		var code int
		select {
		case ev := <-s.events:
			code = s.onEvent(ev)
		case <-s.expiryTick:
			code = exitIf(s.sweepExpired())
		case r := <-s.mediaDone:
			code = exitIf(s.loader.done(r))
		case r := <-s.sent:
			code = exitIf(s.box.done(r))
		case r := <-s.participantsDone:
			code = exitIf(s.box.participantsDone(r))
		case r := <-s.pasteDone:
			code = s.pasted(r)
		case e := <-s.historyEnded:
			code = exitIf(s.hist.timedOut(e))
		case <-s.muteTick:
			s.muteTick, s.muteAt = nil, 0
			code = exitIf(s.send(notification("chatsChanged", nil)))
			s.scheduleMuteEnd()
		case <-s.retryConnect:
			s.retryConnect = nil
			code = exitIf(s.connect())
		case line, ok := <-lines:
			if !ok {
				return exitCode(<-scanErr, logOut)
			}
			code = s.onLine(line)
		}
		if code != keepGoing {
			return code
		}
	}
}

// qrDataURI renders a WhatsApp QR code string as a PNG data: URI.
func qrDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.Q) // Q restores 25% of the code: the Plugin covers the middle with the logo
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
	return errorReplyData(id, code, message, nil)
}

// errorReplyData is errorReply with an `error.data` member. A nil data leaves it out.
func errorReplyData(id json.RawMessage, code int, message string, data any) map[string]any {
	var idValue any // nil encodes as JSON null
	if len(id) > 0 {
		_ = json.Unmarshal(id, &idValue)
	}
	e := map[string]any{"code": code, "message": message}
	if data != nil {
		e["data"] = data
	}
	return map[string]any{"jsonrpc": "2.0", "id": idValue, "error": e}
}
