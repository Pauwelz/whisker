package contract

import (
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
)

// draftGet answers with the Draft of a Chat. A Quote or Edit target that is gone or revoked is dropped, and so is a file that is gone.
func (s *session) draftGet(req *request) outcome {
	chat := req.Params.Chat
	d, err := s.chats.Draft(chat)
	if err != nil {
		fmt.Fprintln(s.logOut, "draft read failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	d.Quote = liveTarget(s.chats, chat, d.Quote)
	d.Edit = liveTarget(s.chats, chat, d.Edit)
	if d.Attachment != "" {
		if _, err := statSendable(d.Attachment); err != nil { // the file is gone; the text stays
			d.Attachment = ""
		}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": chat, "draft": d})}
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

// draftSet stores the Draft of a Chat. Error -32006 means a text that is too long, or a target that is not a stored Message of the Chat.
func (s *session) draftSet(req *request) outcome {
	p := req.Params
	chat, d := p.Chat, chatstore.Draft{Text: p.Text, Quote: p.Quote, Edit: p.Edit, Attachment: p.Attachment}
	invalid := outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	if chat == "" || !fitsMessage(d.Text) {
		return invalid
	}
	if d.Attachment != "" && (d.Edit != "" || d.Quote != "") { // a file never combines with an Edit or a Quote (the Plugin drops one of them)
		return invalid
	}
	// Check the file only when it is new: a keystroke that keeps the stored file must save even if the file is gone now.
	old, oldErr := s.chats.Draft(chat)
	if oldErr != nil || d.Attachment != old.Attachment {
		if d.Attachment != "" {
			if _, err := statSendable(d.Attachment); err != nil {
				return invalid
			}
		}
	}
	for _, target := range []string{d.Quote, d.Edit} {
		if target == "" {
			continue
		}
		if _, err := s.chats.Message(chat, target); err != nil {
			return outcome{reply: s.storeOrInvalid(req.ID, err)}
		}
	}
	if err := s.chats.SetDraft(chat, d); err != nil {
		fmt.Fprintln(s.logOut, "draft write failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	if oldErr == nil && d.Attachment != old.Attachment {
		dropPasted(old.Attachment) // removed, replaced, or dropped for a Quote
	}
	return outcome{reply: emptyReply(req)}
}
