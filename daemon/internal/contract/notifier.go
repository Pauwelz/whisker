package contract

import (
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/notifypolicy"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// mediaLabels name a Message with Media and no text. The keys are the Media kinds of docs/contract.md.
var mediaLabels = map[string]string{
	"image": "Photo", "sticker": "Sticker", "voice": "Voice message", "audio": "Audio", "video": "Video", "document": "Document",
}

// previewHidden is the body when the notificationPreview setting is off.
const previewHidden = "New message"

// notifier shows a desktop notification for a new Message when the policy (notifypolicy) says so.
// It runs on the Daemon loop only.
type notifier struct {
	chats   *chatstore.Store
	prefs   *settings.Store
	attn    *attention.Attention
	desktop desktop.Notifier
	logOut  io.Writer
	tokens  clickTokens
}

// show sends the notification of a stored new Message, or nothing when the policy says no.
func (n *notifier) show(ev whatsapp.NewMessage) {
	prefs, err := n.prefs.Get()
	if err != nil {
		fmt.Fprintln(n.logOut, "settings failed") // no notification without a readable setting
		return
	}
	chat, err := n.chats.Chat(ev.Chat)
	if storeFailed(n.logOut, storeRead, err) {
		return
	}
	if !notifypolicy.Notifies(notifypolicy.Input{
		FromMe:        ev.Message.FromMe,
		Muted:         chat.Muted,
		Archived:      chat.Archived, // read after the store applied the unarchive rule
		Mentioned:     ev.Message.MentionsMe,
		Attended:      ev.Chat == n.attn.Attended(),
		Notifications: prefs.Notifications,
	}) {
		return
	}
	body := previewHidden
	if prefs.NotificationPreview {
		m, err := n.chats.Message(ev.Chat, ev.Message.ID)
		if storeFailed(n.logOut, storeRead, err) {
			return
		}
		body = m.Text
		if body == "" && m.Media != nil {
			body = mediaLabels[string(m.Media.Kind)]
		}
		if chat.Kind == whatsapp.KindGroup && m.SenderName != "" {
			sender := m.SenderName
			if m.SenderPush {
				sender = chatstore.MarkedName(sender, m.Sender)
			}
			body = sender + ": " + body
		}
	}
	summary := chat.Name
	if chat.PushNameOnly {
		summary = chatstore.MarkedName(summary, chat.JID)
	}
	n.desktop.Notify(desktop.Notification{Summary: summary, Body: body, Token: n.tokens.tokenFor(chat.JID)})
}
