package contract

import (
	"fmt"
	"slices"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
)

// maxAttachments is the most Attachments a Draft holds.
const maxAttachments = 30

// draftGet answers with the Draft of a Chat and `unsendable`, the Attachments that `chat.sendMedia` would refuse now.
// A Quote or Edit target that is gone or revoked is dropped. An Attachment is kept: the user removes it.
func (s *session) draftGet(req *request) outcome {
	chat := req.Params.Chat
	d, err := s.chats.Draft(chat)
	if err != nil {
		fmt.Fprintln(s.logOut, "draft read failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	d.Quote = liveTarget(s.chats, chat, d.Quote)
	d.Edit = liveTarget(s.chats, chat, d.Edit)
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": chat, "draft": d, "unsendable": unsendable(d.Attachments)})}
}

// unsendable gives the paths that statSendable refuses. The list is never nil.
func unsendable(paths []string) []string {
	bad := []string{}
	for _, path := range paths {
		if _, err := statSendable(path); err != nil {
			bad = append(bad, path)
		}
	}
	return bad
}

func liveTarget(chats *chatstore.Store, chat, id string) string {
	if id == "" {
		return ""
	}
	if m, err := chats.Message(chat, id); err != nil || inert(m) {
		return ""
	}
	return id
}

// draftSet stores the Draft of a Chat. Error -32006 means a text that is too long, or a target that is not a stored Message of the Chat.
func (s *session) draftSet(req *request) outcome {
	p := req.Params
	chat, d := p.Chat, chatstore.Draft{Text: p.Text, Quote: p.Quote, Edit: p.Edit, Attachments: p.Attachments}
	invalid := outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	if chat == "" || !fitsMessage(d.Text) || len(d.Attachments) > maxAttachments {
		return invalid
	}
	if len(d.Attachments) > 0 && (d.Edit != "" || d.Quote != "") { // files never combine with an Edit or a Quote (the Plugin drops one of them)
		return invalid
	}
	// Check a file only when it is new: a keystroke that keeps a stored file must save even if the file is gone now.
	old, oldErr := s.chats.Draft(chat)
	d.Attachments = slices.Clone(d.Attachments)
	for i, path := range d.Attachments {
		if oldErr == nil && slices.Contains(old.Attachments, path) {
			continue
		}
		real, err := resolveSendable(path)
		if err != nil {
			return invalid
		}
		d.Attachments[i] = real // the Draft keeps the real path, so a later symlink swap is refused at send time
	}
	for _, target := range []string{d.Quote, d.Edit} {
		if target == "" {
			continue
		}
		// A revoked target may stay: liveTarget drops it at the next draft.get. A System Message never is a target.
		if m, err := s.chats.Message(chat, target); err != nil || m.System {
			return outcome{reply: s.storeOrInvalid(req.ID, err)}
		}
	}
	if err := s.chats.SetDraft(chat, d); err != nil {
		fmt.Fprintln(s.logOut, "draft write failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	for _, path := range old.Attachments {
		if !slices.Contains(d.Attachments, path) {
			dropPasted(path) // removed, or dropped for a Quote or an Edit
		}
	}
	return outcome{reply: emptyReply(req)}
}
