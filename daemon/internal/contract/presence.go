package contract

import (
	"fmt"
	"io"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// presenter sends the Presence of the user: `Online` while the pop-out is open, and Composing while the user types.
// "send online" and "send typing" (Profile Settings) gate both. It runs on the Daemon loop only.
// shortcut: the calls block the loop while they send, and a failed call is not repeated until the next change, upgrade if that shows.
// shortcut: Composing is sent once per change, not refreshed while the user keeps typing.
type presenter struct {
	port       whatsapp.Port
	prefs      *settings.Store
	attn       *attention.Attention
	logOut     io.Writer
	known      bool // WhatsApp has our availability for this connection
	online     bool // that availability
	subscribed map[string]bool
}

func newPresenter(port whatsapp.Port, prefs *settings.Store, attn *attention.Attention, logOut io.Writer) *presenter {
	return &presenter{port: port, prefs: prefs, attn: attn, logOut: logOut, subscribed: map[string]bool{}}
}

// connected runs after each new connection. WhatsApp needs the availability once per connection.
func (p *presenter) connected() {
	p.known = false
	p.subscribed = map[string]bool{}
	p.sync()
}

// sync sends the availability when it differs from what WhatsApp has, and subscribes to the Attended Chat.
func (p *presenter) sync() {
	prefs, err := p.prefs.Get()
	if err != nil {
		fmt.Fprintln(p.logOut, "settings failed") // no Presence without a readable setting
		return
	}
	if want := p.attn.Open() && prefs.SendOnline; !p.known || want != p.online {
		if err := p.port.SendPresence(want); err != nil {
			fmt.Fprintln(p.logOut, "presence failed")
		} else {
			p.known, p.online = true, want
		}
	}
	if chat := p.attn.Attended(); chat != "" && !strings.HasSuffix(chat, "@g.us") && !p.subscribed[chat] {
		if err := p.port.SubscribePresence(chat); err != nil {
			fmt.Fprintln(p.logOut, "presence subscribe failed")
		} else {
			p.subscribed[chat] = true
		}
	}
}

// typing sends Composing or Paused to a Chat, if "send typing" is on.
func (p *presenter) typing(chat string, composing bool) {
	prefs, err := p.prefs.Get()
	if err != nil || !prefs.SendTyping {
		return
	}
	if err := p.port.SendTyping(chat, composing); err != nil {
		fmt.Fprintln(p.logOut, "typing failed")
	}
}
