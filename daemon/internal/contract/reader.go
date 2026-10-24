package contract

import (
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/receiptpolicy"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// reader clears Read Markers and sends Read Receipts for the Attended Chat.
// The policy (receiptpolicy) decides when. It runs on the Daemon loop only.
// shortcut: MarkRead and MarkChatRead block the loop while they send, upgrade if a slow network shows.
// shortcut: a Read Receipt that fails stays due and goes out with the next one for that Chat.
type reader struct {
	chats  *chatstore.Store
	port   whatsapp.Port
	prefs  *settings.Store
	attn   *attention.Attention
	logOut io.Writer
	played map[string]bool // Played sent, by Chat and Message; lost at restart
}

// setAttention records the pop-out state. It reports whether the Chat list changed (a Read Marker cleared).
func (r *reader) setAttention(open bool, chat string) bool {
	r.attn.Set(open, chat)
	return r.attend(receiptpolicy.Attended)
}

// incoming runs for each new Message. A Message in the Attended Chat is read at once.
func (r *reader) incoming(ev whatsapp.NewMessage) {
	if !ev.Message.FromMe && ev.Chat == r.attn.Attended() {
		r.attend(receiptpolicy.Incoming)
	}
}

// interaction runs when the user sent a Message in a Chat.
func (r *reader) interaction(chat string) { r.receipts(chat, receiptpolicy.Interaction) }

// attend clears the Read Marker of the Attended Chat in every mode, then sends Read Receipts if due.
func (r *reader) attend(t receiptpolicy.Trigger) bool {
	chat := r.attn.Attended()
	if chat == "" {
		return false
	}
	cleared, err := r.chats.ClearUnread(chat)
	if err != nil {
		fmt.Fprintln(r.logOut, "chat store write failed")
	} else if cleared {
		if err := r.port.MarkChatRead(chat); err != nil {
			fmt.Fprintln(r.logOut, "read marker failed")
		}
	}
	r.receipts(chat, t)
	return cleared
}

// receipts sends a Read Receipt for each unread Message of the Chat, one call per sender, when the policy says so.
func (r *reader) receipts(chat string, t receiptpolicy.Trigger) {
	prefs, err := r.prefs.Get()
	if err != nil {
		fmt.Fprintln(r.logOut, "settings failed") // no Read Receipt without a readable mode
		return
	}
	if !receiptpolicy.Due(prefs.ReadReceipts, t, chat == r.attn.Attended()) {
		return
	}
	unread, err := r.chats.Unreceipted(chat)
	if err != nil {
		fmt.Fprintln(r.logOut, "chat store read failed")
		return
	}
	var senders []string // in order of first Message
	ids := map[string][]string{}
	for _, u := range unread {
		if ids[u.Sender] == nil {
			senders = append(senders, u.Sender)
		}
		ids[u.Sender] = append(ids[u.Sender], u.ID)
	}
	for _, sender := range senders {
		if err := r.port.MarkRead(chat, sender, ids[sender]); err != nil {
			fmt.Fprintln(r.logOut, "read receipt failed")
			continue
		}
		if err := r.chats.Receipted(chat, ids[sender]); err != nil {
			fmt.Fprintln(r.logOut, "chat store write failed")
		}
	}
}

// playedNote runs when the user starts a voice note of another Participant. It sends Played, and Read for that Message only, as the mode says.
// shortcut: a repeat play after a Daemon restart sends Played again, which WhatsApp ignores; store the flag if that shows.
func (r *reader) playedNote(chat, sender, id string) {
	key := chat + "\x00" + id
	if r.played[key] {
		return
	}
	prefs, err := r.prefs.Get()
	if err != nil {
		fmt.Fprintln(r.logOut, "settings failed")
		return
	}
	played, read := receiptpolicy.Played(prefs.ReadReceipts)
	if !played {
		return
	}
	if read {
		if err := r.port.MarkRead(chat, sender, []string{id}); err != nil {
			fmt.Fprintln(r.logOut, "read receipt failed")
			return
		}
		if err := r.chats.Receipted(chat, []string{id}); err != nil {
			fmt.Fprintln(r.logOut, "chat store write failed")
		}
	}
	if err := r.port.MarkPlayed(chat, sender, id); err != nil {
		fmt.Fprintln(r.logOut, "played receipt failed")
		return
	}
	r.played[key] = true
}
