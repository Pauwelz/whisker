package contract

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// deletedForMe applies a Delete for me from another device of the user. An unknown Message changes nothing.
// The Plugin takes the Message out of the open Chat on `messageExpired`.
func (s *session) deletedForMe(ev whatsapp.DeletedForMe) bool {
	if _, err := s.chats.Message(ev.Chat, ev.ID); errors.Is(err, sql.ErrNoRows) {
		return true
	} else if err != nil {
		fmt.Fprintln(s.logOut, "chat store read failed")
		return true
	}
	if err := s.chats.DeleteMessage(ev.Chat, ev.ID); err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	return s.send(notification("messageExpired", map[string]any{"chat": ev.Chat, "id": ev.ID}))
}

// chatCleared applies a Clear chat from another device. The Plugin hears of it only when a Message went.
func (s *session) chatCleared(ev whatsapp.ChatCleared) bool {
	changed, err := s.chats.ClearChat(ev.Chat, ev.Until)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	return !changed || s.send(notification("chatCleared", map[string]any{"chat": ev.Chat, "until": ev.Until}))
}

// chatDeleted applies a Delete chat from another device. The Plugin hears of it only when something went.
// A Message newer than the delete stays, with its Chat: the delete is an old one, or it came before the Message.
// That case is a Clear chat.
func (s *session) chatDeleted(ev whatsapp.ChatDeleted) bool {
	if ev.Until > 0 {
		newer, err := s.chats.HasMessageAfter(ev.Chat, ev.Until)
		if err != nil {
			fmt.Fprintln(s.logOut, "chat store read failed")
			return true
		}
		if newer {
			return s.chatCleared(whatsapp.ChatCleared{Chat: ev.Chat, Until: ev.Until})
		}
	}
	attachments, gone, err := s.chats.DeleteChat(ev.Chat)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	for _, path := range attachments {
		dropPasted(path)
	}
	if !gone {
		return true
	}
	s.scheduleMuteEnd()
	return s.send(notification("chatRemoved", map[string]any{"chat": ev.Chat}))
}
