package contract

import (
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// livePolicy is the one place that decides what a live Message sets in motion:
// Media of an automatic kind downloads at once when autoDownload is on, and a Message of another Participant marks its Chat unread.
func livePolicy(ev whatsapp.NewMessage, autoDownload bool) chatstore.LivePolicy {
	p := chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: !ev.Message.FromMe}
	if m := ev.Message.Media; m != nil && m.Kind.Auto() && autoDownload {
		p.MediaState = chatstore.MediaDownloading
	}
	return p
}
