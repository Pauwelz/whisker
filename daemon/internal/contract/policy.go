package contract

import (
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// livePolicy is the one place that decides what a live Message sets in motion:
// Media of an automatic kind downloads at once when the settings allow its size, and a Message of another Participant marks
// its Chat unread. autoDownloads is settings.Settings.AutoDownloads of the current settings.
func livePolicy(ev whatsapp.NewMessage, autoDownloads func(size int64) bool) chatstore.LivePolicy {
	p := chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: !ev.Message.FromMe}
	if m := ev.Message.Media; m != nil && m.Kind.Auto() && autoDownloads(m.Size) {
		p.MediaState = chatstore.MediaDownloading
	}
	return p
}
