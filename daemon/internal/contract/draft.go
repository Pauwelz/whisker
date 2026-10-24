package contract

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
)

// draftGetReply answers with the Draft of a Chat. A Quote or Edit target that is gone or revoked is dropped, and so is a file that is gone.
func draftGetReply(chats *chatstore.Store, id json.RawMessage, chat string, logOut io.Writer) map[string]any {
	d, err := chats.Draft(chat)
	if err != nil {
		fmt.Fprintln(logOut, "draft read failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	d.Quote = liveTarget(chats, chat, d.Quote)
	d.Edit = liveTarget(chats, chat, d.Edit)
	if d.Attachment != "" {
		if _, err := statSendable(d.Attachment); err != nil { // the file is gone; the text stays
			d.Attachment = ""
		}
	}
	return resultReply(id, map[string]any{"chat": chat, "draft": d})
}

func liveTarget(chats *chatstore.Store, chat, id string) string {
	if id == "" {
		return ""
	}
	if m, err := chats.Message(chat, id); err != nil || m.Revoked {
		return ""
	}
	return id
}

// draftSetReply stores the Draft of a Chat. Error -32006 means a text that is too long, or a target that is not a stored Message of the Chat.
func draftSetReply(chats *chatstore.Store, id json.RawMessage, chat string, d chatstore.Draft, logOut io.Writer) map[string]any {
	if chat == "" || !fitsMessage(d.Text) {
		return errorReply(id, errMessage, msgInvalidMessage)
	}
	if d.Attachment != "" && (d.Edit != "" || d.Quote != "") { // a file never combines with an Edit or a Quote
		return errorReply(id, errMessage, msgInvalidMessage)
	}
	// Check the file only when it is new: a keystroke that keeps the stored file must save even if the file is gone now.
	if old, err := chats.Draft(chat); err != nil || d.Attachment != old.Attachment {
		if d.Attachment != "" {
			if _, err := statSendable(d.Attachment); err != nil {
				return errorReply(id, errMessage, msgInvalidMessage)
			}
		}
	}
	for _, target := range []string{d.Quote, d.Edit} {
		if target == "" {
			continue
		}
		if _, err := chats.Message(chat, target); err != nil {
			return storeOrInvalid(id, err, logOut)
		}
	}
	if err := chats.SetDraft(chat, d); err != nil {
		fmt.Fprintln(logOut, "draft write failed")
		return errorReply(id, errChatStore, msgChatStore)
	}
	return resultReply(id, map[string]any{})
}
