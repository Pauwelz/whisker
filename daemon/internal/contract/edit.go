package contract

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// EditWindow is how long after sending the user can edit a Message. WhatsApp enforces the same limit.
const EditWindow = 15 * time.Minute

// sentByWhatsApp tells that a Message of the user is in one of the Delivery States after WhatsApp accepted it.
func sentByWhatsApp(state string) bool {
	return state != "" && state != whatsapp.StatePending && state != whatsapp.StateFailed
}

// chatEdit edits a Message of the user. A Message with mentions is refused: the edit would send the names as plain text. The follow-up sends `messageChanged` for it.
// shortcut: the WhatsApp call blocks the Daemon loop (up to 15 s), upgrade when another request must run meanwhile.
func (s *session) chatEdit(req *request) outcome {
	p := req.Params
	m, err := s.chats.Message(p.Chat, p.ID)
	if err != nil || !m.FromMe || inert(m) || !sentByWhatsApp(m.State) || len(m.Mentions) > 0 ||
		!validText(p.Text) ||
		Clock.Now().Sub(time.Unix(m.Time, 0)) > EditWindow {
		return outcome{reply: s.storeOrInvalid(req.ID, err)}
	}
	if err := s.port.Edit(p.Chat, p.ID, p.Text); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return s.changed(req.ID, p.Chat, p.ID, func() (bool, error) {
		return s.chats.Edit(whatsapp.MessageEdited{Chat: p.Chat, ID: p.ID, FromMe: true, Text: p.Text, Time: Clock.Now().Unix()}, EditWindow)
	}, func() { s.clearDraft(p.Chat) })
}

// chatRevoke revokes a Message of the user for everyone. It becomes a Tombstone.
func (s *session) chatRevoke(req *request) outcome {
	p := req.Params
	m, err := s.chats.Message(p.Chat, p.ID)
	if err != nil || !m.FromMe || inert(m) || !sentByWhatsApp(m.State) {
		return outcome{reply: s.storeOrInvalid(req.ID, err)}
	}
	if err := s.port.Revoke(p.Chat, p.ID); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return s.changed(req.ID, p.Chat, p.ID, func() (bool, error) { return s.chats.Revoke(p.Chat, p.ID, "", true) }, nil)
}

// inert tells that no action applies to a Message: a Tombstone or a System Message. It shows as not found.
func inert(m chatstore.Message) bool { return m.Revoked || m.System }

// storeOrInvalid answers a failed lookup with "chat store failed", and any other refusal with "invalid message".
func (s *session) storeOrInvalid(id json.RawMessage, err error) map[string]any {
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintln(s.logOut, "message read failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	return errorReply(id, errMessage, msgInvalidMessage)
}

// whatsappFailed answers a failed WhatsApp call: no connection, a timeout or any other refusal. Nothing changed.
func (s *session) whatsappFailed(id json.RawMessage, err error) map[string]any {
	fmt.Fprintln(s.logOut, "whatsapp call failed") // the error text could hold names
	return errorReply(id, errNoWhatsApp, msgNoWhatsApp)
}

// changed stores the change and answers with the new Message. The follow-up runs before and then tells the Plugin it changed.
func (s *session) changed(id json.RawMessage, chat, msgID string, change func() (bool, error), before func()) outcome {
	if _, err := change(); err != nil {
		fmt.Fprintln(s.logOut, "message write failed")
		return outcome{reply: errorReply(id, errChatStore, msgChatStore)}
	}
	m, err := s.chats.Message(chat, msgID)
	reply, changed := s.messageReply(id, m, err)
	return s.followUp(reply, oneOf(changed), before, s.announce, nil)
}
