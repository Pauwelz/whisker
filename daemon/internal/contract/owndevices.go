package contract

import (
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ownDevices shows a notice when the account of the user has a device that no earlier list held.
// The set is updated first: a failed notice is not shown again after the next connection.
func (s *session) ownDevices(ev whatsapp.OwnDevices) bool {
	if ev.Failed {
		fmt.Fprintln(s.logOut, "own device list failed") // the error text could hold ids
		return true
	}
	added, err := s.chats.NoteOwnDevices(ev.IDs)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	return !added || s.box.systemMessage(whatsapp.NewMessage{Chat: ev.Chat, Kind: whatsapp.KindSelf, Message: ev.Notice})
}
