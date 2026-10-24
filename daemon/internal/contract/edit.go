package contract

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// chatEditReply edits a Message of the user. The caller sends `messageChanged` for the returned Message.
// shortcut: the WhatsApp call blocks the Daemon loop (up to 15 s), upgrade when another request must run meanwhile.
func chatEditReply(chats *chatstore.Store, port whatsapp.Port, id json.RawMessage, chat, msgID, text string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	m, err := chats.Message(chat, msgID)
	if err != nil || !m.FromMe || m.Revoked || !sentByWhatsApp(m.State) ||
		!validText(text) ||
		Clock.Now().Sub(time.Unix(m.Time, 0)) > EditWindow {
		return storeOrInvalid(id, err, logOut), nil
	}
	if err := port.Edit(chat, msgID, text); err != nil {
		return sendFailed(id, err, logOut), nil
	}
	return changedReply(id, logOut, chats, chat, msgID, func() (bool, error) { return chats.Edit(chat, msgID, "", true, text) })
}

// chatRevokeReply revokes a Message of the user for everyone. It becomes a Tombstone.
func chatRevokeReply(chats *chatstore.Store, port whatsapp.Port, id json.RawMessage, chat, msgID string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	m, err := chats.Message(chat, msgID)
	if err != nil || !m.FromMe || m.Revoked || !sentByWhatsApp(m.State) {
		return storeOrInvalid(id, err, logOut), nil
	}
	if err := port.Revoke(chat, msgID); err != nil {
		return sendFailed(id, err, logOut), nil
	}
	return changedReply(id, logOut, chats, chat, msgID, func() (bool, error) { return chats.Revoke(chat, msgID) })
}

// storeOrInvalid answers a failed lookup with "chat store failed", and any other refusal with "invalid message".
func storeOrInvalid(id json.RawMessage, err error, logOut io.Writer) map[string]any {
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintln(logOut, "message read failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	return errorReply(id, errMessage, msgInvalidMessage)
}

func sendFailed(id json.RawMessage, err error, logOut io.Writer) map[string]any {
	fmt.Fprintln(logOut, "edit or revoke failed") // the error text could hold names
	if errors.Is(err, whatsapp.ErrOffline) {
		return errorReply(id, errSendFailed, msgNotConnected)
	}
	return errorReply(id, errSendFailed, msgSendFailed)
}

// changedReply stores the change and answers with the new Message.
func changedReply(id json.RawMessage, logOut io.Writer, chats *chatstore.Store, chat, msgID string, change func() (bool, error)) (map[string]any, *chatstore.Message) {
	if _, err := change(); err != nil {
		fmt.Fprintln(logOut, "message write failed")
		return errorReply(id, errChatStore, msgChatStore), nil
	}
	m, err := chats.Message(chat, msgID)
	return messageReply(id, m, err, logOut)
}
