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
	"strings"
	"unicode/utf8"

	"rsc.io/qr"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
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
	errInvalidPhone   = -32001
	errPairCode       = -32002
	errChatStore      = -32003
	errSetting        = -32004
	errMessage        = -32006
	errSendFailed     = -32007
	errMedia          = -32008
	errMediaFailed    = -32009
)

// The message of each application error. A code that has two messages has two constants.
const (
	msgParse          = "parse error"
	msgMethodNotFound = "method not found"
	msgInvalidPhone   = "invalid phone number"
	msgPairCode       = "pair code failed"
	msgChatStore      = "chat store failed"
	msgInvalidSetting = "invalid setting"
	msgSettingsFailed = "settings failed"
	msgInvalidMessage = "invalid message"
	msgNotConnected   = "not connected"
	msgSendFailed     = "send failed"
	msgInvalidMedia   = "invalid media"
	msgMediaIntegrity = "media failed the integrity check"
	msgMediaCopy      = "media copy failed"
)

// qrScale is the pixel size of one QR module.
const qrScale = 8

// Profile is what Run needs from the Profile: open the stores, the chat store, and delete the Profile.
// Open returns keystore.ErrUnavailable when the keyring does not answer. Chat is valid after Open.
type Profile interface {
	Open() error
	Chat() *sql.DB
	Media() *media.Store
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
	s := &session{profile: profile, prefs: prefs, newPort: newPort, logOut: logOut}
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

func pairCodeReply(port whatsapp.Port, id json.RawMessage, phone string, logOut io.Writer) map[string]any {
	// shortcut: PairCode blocks the Daemon loop (up to 30 s), upgrade when another request must run meanwhile.
	code, err := port.PairCode(phone)
	switch {
	case errors.Is(err, whatsapp.ErrInvalidPhone):
		return errorReply(id, errInvalidPhone, msgInvalidPhone)
	case err != nil:
		fmt.Fprintln(logOut, "pair code failed") // the error text could hold the phone number
		return errorReply(id, errPairCode, msgPairCode)
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
		return errorReply(id, errChatStore, msgChatStore)
	}
	unread, err := chats.UnreadChats()
	if err != nil {
		fmt.Fprintln(logOut, "chat list failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	return resultReply(id, map[string]any{"chats": page.Chats, "more": page.More, "offset": offset, "unreadChats": unread})
}

func chatMessagesReply(chats *chatstore.Store, id json.RawMessage, chat string, offset, limit int, logOut io.Writer) map[string]any {
	offset = max(offset, 0)
	if limit <= 0 {
		limit = defaultChatsLimit
	}
	page, err := chats.Messages(chat, offset, min(limit, maxChatsLimit))
	if err != nil {
		fmt.Fprintln(logOut, "message list failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	return resultReply(id, map[string]any{"chat": chat, "messages": page.Messages, "more": page.More, "offset": offset})
}

// chatSendReply stores the text as a pending Message. The caller sends it after the reply.
// A non-empty quoteID makes it a reply: that Message must exist in the Chat and not be a Tombstone.
func chatSendReply(chats *chatstore.Store, port whatsapp.Port, id json.RawMessage, chat, text, quoteID string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	if !validText(text) {
		return errorReply(id, errMessage, msgInvalidMessage), nil
	}
	var quote *whatsapp.Quote
	if quoteID != "" {
		q, err := chats.Message(chat, quoteID)
		if err != nil || q.Revoked {
			return storeOrInvalid(id, err, logOut), nil
		}
		quote = whatsappQuote(q.ID, q.Sender, q.Text)
	}
	m, err := chats.AddPending(chat, port.NewMessageID(), text, quote)
	return messageReply(id, m, err, logOut)
}

// chatRetryReply makes a failed Message pending again. The caller sends it after the reply.
func chatRetryReply(chats *chatstore.Store, id json.RawMessage, chat, msgID string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	changed, err := chats.Requeue(chat, msgID)
	if err == nil && !changed {
		return errorReply(id, errMessage, msgInvalidMessage), nil
	}
	if err != nil {
		return messageReply(id, chatstore.Message{}, err, logOut)
	}
	m, err := chats.Message(chat, msgID)
	return messageReply(id, m, err, logOut)
}

func messageReply(id json.RawMessage, m chatstore.Message, err error, logOut io.Writer) (map[string]any, *chatstore.Message) {
	switch {
	case errors.Is(err, chatstore.ErrNoChat):
		return errorReply(id, errMessage, msgInvalidMessage), nil
	case err != nil:
		fmt.Fprintln(logOut, "message write failed")
		return errorReply(id, errChatStore, msgChatStore), nil
	}
	return resultReply(id, map[string]any{"message": m}), &m
}

func settingsReply(id json.RawMessage, logOut io.Writer, apply func() (settings.Settings, error)) map[string]any {
	cur, err := apply()
	switch {
	case errors.Is(err, settings.ErrInvalid):
		return errorReply(id, errSetting, msgInvalidSetting)
	case err != nil:
		fmt.Fprintln(logOut, "settings failed") // the error text could name paths
		return errorReply(id, errSetting, msgSettingsFailed)
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
