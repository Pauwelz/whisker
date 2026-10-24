package contract

import (
	"slices"
)

// ReactionEmoji is the Reaction picker: the six default emoji of WhatsApp. An empty emoji removes the Reaction.
var ReactionEmoji = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// chatReact sets or removes the Reaction of the user on a Message. The follow-up sends `messageChanged` for it.
// A Reaction is an interaction, like a sent Message.
// shortcut: the WhatsApp call blocks the Daemon loop (up to 15 s), like chat.edit.
func (s *session) chatReact(req *request) outcome {
	p := req.Params
	m, err := s.chats.Message(p.Chat, p.ID)
	if err != nil || m.Revoked || (p.Emoji != "" && !slices.Contains(ReactionEmoji, p.Emoji)) {
		return outcome{reply: s.storeOrInvalid(req.ID, err)}
	}
	if err := s.port.React(p.Chat, p.ID, m.Sender, p.Emoji); err != nil {
		return outcome{reply: s.sendFailed(req.ID, err)}
	}
	return s.changed(req.ID, p.Chat, p.ID, func() (bool, error) { return s.chats.SetReaction(p.Chat, p.ID, "", true, p.Emoji) }, func() { s.read.interaction(p.Chat) })
}
