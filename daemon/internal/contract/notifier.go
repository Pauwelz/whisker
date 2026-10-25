package contract

import (
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/notifypolicy"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// notifier turns a new Message into a `notify` frame when the policy (notifypolicy) says so.
// The Plugin shows the desktop notification. It runs on the Daemon loop only.
// shortcut: a live Message that arrives twice notifies twice, upgrade if WhatsApp repeats events.
type notifier struct {
	chats  *chatstore.Store
	prefs  *settings.Store
	attn   *attention.Attention
	logOut io.Writer
}

// frame returns the `notify` frame for a stored new Message, or nil when it does not notify.
func (n *notifier) frame(ev whatsapp.NewMessage) map[string]any {
	prefs, err := n.prefs.Get()
	if err != nil {
		fmt.Fprintln(n.logOut, "settings failed") // no notification without a readable setting
		return nil
	}
	chat, err := n.chats.Chat(ev.Chat)
	if err != nil {
		fmt.Fprintln(n.logOut, "chat store read failed")
		return nil
	}
	if !notifypolicy.Notifies(notifypolicy.Input{
		FromMe:        ev.Message.FromMe,
		Muted:         chat.Muted,
		Archived:      chat.Archived, // read after the store applied the unarchive rule
		Mentioned:     ev.Message.MentionsMe,
		Attended:      ev.Chat == n.attn.Attended(),
		Notifications: prefs.Notifications,
	}) {
		return nil
	}
	m, err := n.chats.Message(ev.Chat, ev.Message.ID)
	if err != nil {
		fmt.Fprintln(n.logOut, "chat store read failed")
		return nil
	}
	params := map[string]any{"chat": chat.JID, "kind": chat.Kind, "name": chat.Name, "senderName": m.SenderName, "text": m.Text}
	if m.Media != nil {
		params["media"] = m.Media.Kind // the Plugin names the Media when there is no text
	}
	return notification("notify", params)
}
