package contract

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// maxPinned is the WhatsApp limit of Pinned Chats.
const maxPinned = 3

// chatPin pins or unpins a Chat. The store changes only after WhatsApp accepted the write.
func (s *session) chatPin(req *request) outcome {
	p := req.Params
	chat, refusal := s.knownChat(req, p.Chat)
	if refusal != nil {
		return outcome{reply: refusal}
	}
	if p.Pinned && !chat.Pinned {
		n, err := s.chats.PinnedCount()
		if err != nil {
			return outcome{reply: s.storeFailed(req)}
		}
		if n >= maxPinned {
			return outcome{reply: errorReply(req.ID, errPinLimit, msgPinLimit)}
		}
	}
	if err := s.port.SetPinned(p.Chat, p.Pinned); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return s.applied(req, whatsapp.Pin{JID: p.Chat, Pinned: p.Pinned})
}

// chatMute mutes a Chat until a time, for ever (-1), or unmutes it (0).
func (s *session) chatMute(req *request) outcome {
	p := req.Params
	if _, refusal := s.knownChat(req, p.Chat); refusal != nil {
		return outcome{reply: refusal}
	}
	if p.Until == nil || (*p.Until != -1 && *p.Until != 0 && *p.Until <= Clock.Now().Unix()) {
		return outcome{reply: errorReply(req.ID, errInvalidParams, msgInvalidParams)}
	}
	if err := s.port.SetMuted(p.Chat, *p.Until); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return s.applied(req, whatsapp.Mute{JID: p.Chat, Until: *p.Until})
}

// chatMarkUnread sets or clears the Read Marker of a Chat. It sends no Read Receipt.
func (s *session) chatMarkUnread(req *request) outcome {
	p := req.Params
	if _, refusal := s.knownChat(req, p.Chat); refusal != nil {
		return outcome{reply: refusal}
	}
	if err := s.port.SetUnread(p.Chat, p.Unread); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return s.applied(req, whatsapp.ReadMarker{JID: p.Chat, Unread: p.Unread})
}

// chatDeleteForMe deletes a Message on the user's devices only. The store changes after WhatsApp accepted the write.
// The Plugin takes the Message out of the open Chat on `messageExpired`.
func (s *session) chatDeleteForMe(req *request) outcome {
	p := req.Params
	if _, refusal := s.knownChat(req, p.Chat); refusal != nil {
		return outcome{reply: refusal}
	}
	m, err := s.chats.Message(p.Chat, p.ID)
	if err != nil {
		return outcome{reply: s.storeOrInvalid(req.ID, err)}
	}
	if err := s.port.DeleteForMe(p.Chat, p.ID, m.Sender, m.FromMe, m.Time); err != nil {
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	if err := s.chats.DeleteMessage(p.Chat, p.ID); err != nil {
		return outcome{reply: s.storeFailed(req)}
	}
	return outcome{emptyReply(req), func() int {
		return exitIf(s.send(notification("messageExpired", map[string]any{"chat": p.Chat, "id": p.ID})))
	}}
}

// knownChat looks up the Chat of a request. The refusal is "invalid chat" for an unknown Chat.
func (s *session) knownChat(req *request, jid string) (chatstore.Chat, map[string]any) {
	chat, err := s.chats.Chat(jid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return chat, errorReply(req.ID, errInvalidChat, msgInvalidChat)
	case err != nil:
		return chat, s.storeFailed(req)
	}
	return chat, nil
}

func (s *session) storeFailed(req *request) map[string]any {
	fmt.Fprintln(s.logOut, "chat store failed")
	return errorReply(req.ID, errChatStore, msgChatStore)
}

// applied stores an App State change that WhatsApp accepted, then tells the Plugin the Chat list changed.
func (s *session) applied(req *request, ev whatsapp.Event) outcome {
	if _, err := s.chats.Apply(ev); err != nil {
		return outcome{reply: s.storeFailed(req)}
	}
	return outcome{emptyReply(req), func() int { return exitIf(s.send(notification("chatsChanged", nil))) }}
}
