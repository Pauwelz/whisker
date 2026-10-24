package contract

import (
	"encoding/json"
	"io"
	"slices"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ReactionEmoji is the Reaction picker: the six default emoji of WhatsApp. An empty emoji removes the Reaction.
var ReactionEmoji = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// chatReactReply sets or removes the Reaction of the user on a Message. The caller sends `messageChanged` for the returned Message.
// shortcut: the WhatsApp call blocks the Daemon loop (up to 15 s), like chat.edit.
func chatReactReply(chats *chatstore.Store, port whatsapp.Port, id json.RawMessage, chat, msgID, emoji string, logOut io.Writer) (map[string]any, *chatstore.Message) {
	m, err := chats.Message(chat, msgID)
	if err != nil || m.Revoked || (emoji != "" && !slices.Contains(ReactionEmoji, emoji)) {
		return storeOrInvalid(id, err, logOut), nil
	}
	if err := port.React(chat, msgID, m.Sender, emoji); err != nil {
		return sendFailed(id, err, logOut), nil
	}
	return changedReply(id, logOut, chats, chat, msgID, func() (bool, error) { return chats.SetReaction(chat, msgID, "", true, emoji) })
}
