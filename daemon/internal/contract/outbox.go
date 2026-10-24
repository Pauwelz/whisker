package contract

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// Now is the clock of the chat store. Tests replace it to get fixed Message times.
var Now = time.Now

// Timers makes the timers of the Disappearing Message sweep. Tests replace it with a manual clock.
var Timers clock.Clock = clock.Real

// ExpiryInterval is how often the Daemon deletes Disappearing Messages whose timer ended.
// shortcut: an expired Message can show for up to one interval, upgrade to a timer for the next deadline if that matters.
const ExpiryInterval = time.Minute

// ConnectRetryDelay is the wait before the Daemon connects again after a failed connect.
var ConnectRetryDelay = 5 * time.Second

// sendPort is what the outbox needs from WhatsApp.
type sendPort interface {
	Send(chat, id, text string, quote *whatsapp.Quote, timer int64) error
	SendMedia(chat, id string, m whatsapp.Media, plain *os.File, caption string, timer int64) error
}

// outbox sends the Messages of the user and follows the connection.
// A Message waits as `pending` in the chat store until the connection is up, so a restart loses nothing.
// A send and its retries use one Message ID, so WhatsApp drops a repeat: a Message never shows twice.
// It runs on the Daemon loop only. Only the sends run on other goroutines.
type outbox struct {
	chats    *chatstore.Store
	port     sendPort
	send     func(map[string]any) bool
	logOut   io.Writer
	online   bool
	gen      int // counts the Connected events, so a send can tell that a reconnect came while it ran
	inflight map[string]bool
	results  chan sendResult
	store    *media.Store                             // holds the files of Media to send
	incoming func(whatsapp.NewMessage)                // runs after a new Message is stored
	notice   func(whatsapp.NewMessage) map[string]any // the `notify` frame of a new Message, or nil
}

type sendResult struct {
	chat, id string
	gen      int
	err      error
}

func newOutbox(chats *chatstore.Store, port sendPort, store *media.Store, send func(map[string]any) bool, logOut io.Writer,
	incoming func(whatsapp.NewMessage), notice func(whatsapp.NewMessage) map[string]any) *outbox {
	return &outbox{chats: chats, port: port, store: store, send: send, logOut: logOut, incoming: incoming, notice: notice,
		inflight: map[string]bool{}, results: make(chan sendResult, 64)}
}

// submit starts the send of a pending Message. It does nothing offline, or while that Message is in flight.
func (b *outbox) submit(m chatstore.Message) {
	if !b.online || b.inflight[m.ID] {
		return
	}
	b.inflight[m.ID] = true
	gen := b.gen
	timer := max(m.ExpiresAt-m.Time, 0)
	if m.Media != nil {
		ref, err := b.chats.MediaOf(m.Chat, m.ID)
		go func() {
			if err == nil {
				err = b.sendMedia(m, ref.Media, timer)
			}
			b.results <- sendResult{m.Chat, m.ID, gen, err}
		}()
		return
	}
	var quote *whatsapp.Quote
	if m.Quote != nil {
		quote = whatsappQuote(m.Quote.ID, m.Quote.Sender, m.Quote.Text)
	}
	go func() {
		b.results <- sendResult{m.Chat, m.ID, gen, b.port.Send(m.Chat, m.ID, m.Text, quote, timer)}
	}()
}

// sendMedia decrypts the stored file to a plain copy for the upload and removes the copy after.
func (b *outbox) sendMedia(m chatstore.Message, ref whatsapp.Media, timer int64) error {
	path, err := b.store.Plain(m.Chat, m.ID, extension(ref), ref.Kind, ref.Key)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return b.port.SendMedia(m.Chat, m.ID, ref, f, m.Text, timer)
}

// flush starts the send of every pending Message, oldest first.
func (b *outbox) flush() {
	pending, err := b.chats.Pending()
	if err != nil {
		fmt.Fprintln(b.logOut, "chat store read failed")
	}
	for _, m := range pending {
		b.submit(m)
	}
}

// done handles the end of a send. It returns false when a frame could not be written.
func (b *outbox) done(r sendResult) bool {
	delete(b.inflight, r.id)
	switch {
	case r.err == nil:
		return b.move(r.chat, r.id, func() (bool, error) { return b.chats.Advance(r.chat, r.id, whatsapp.StateSent) })
	case errors.Is(r.err, whatsapp.ErrOffline):
		// The Message stays pending. After a reconnect it goes out now, otherwise on the next Connected.
		if r.gen == b.gen {
			b.online = false
		} else {
			b.flush()
		}
		return true
	}
	fmt.Fprintln(b.logOut, "send failed") // the error text could hold names
	return b.move(r.chat, r.id, func() (bool, error) { return b.chats.Fail(r.chat, r.id) })
}

// move runs a state change and tells the Plugin when the Message changed.
func (b *outbox) move(chat, id string, change func() (bool, error)) bool {
	changed, err := change()
	if err != nil {
		fmt.Fprintln(b.logOut, "chat store write failed")
		return true
	}
	return !changed || b.notify(chat, id)
}

// notify sends `messageChanged` for the stored Message.
func (b *outbox) notify(chat, id string) bool {
	m, err := b.chats.Message(chat, id)
	if err != nil {
		fmt.Fprintln(b.logOut, "chat store read failed")
		return true
	}
	return b.send(notification("messageChanged", map[string]any{"message": m}))
}

// connected runs when the connection is up: the Plugin hears of it, then every pending Message goes out.
// Each method below returns false when a frame could not be written.
func (b *outbox) connected() bool {
	b.online = true
	b.gen++
	if !b.send(notification("connected", nil)) {
		return false
	}
	b.flush()
	return true
}

func (b *outbox) disconnected() bool {
	b.online = false
	return b.send(notification("disconnected", nil))
}

// incomingMessage stores a new Message and tells the Plugin: the Message, the Chat list, then the notification if the policy says so.
func (b *outbox) incomingMessage(ev whatsapp.NewMessage) bool {
	if err := b.chats.PutLive(ev, livePolicy(ev)); err != nil {
		fmt.Fprintln(b.logOut, "chat store write failed") // the error text could hold names
		return true
	}
	b.incoming(ev)
	if !b.notify(ev.Chat, ev.Message.ID) || !b.send(notification("chatsChanged", nil)) {
		return false
	}
	if frame := b.notice(ev); frame != nil {
		return b.send(frame)
	}
	return true
}

func (b *outbox) edited(ev whatsapp.MessageEdited) bool {
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.Edit(ev.Chat, ev.ID, ev.Sender, ev.FromMe, ev.Text) })
}

func (b *outbox) revoked(ev whatsapp.MessageRevoked) bool {
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.Revoke(ev.Chat, ev.ID) })
}

func (b *outbox) reacted(ev whatsapp.Reaction) bool {
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.SetReaction(ev.Chat, ev.ID, ev.Sender, ev.FromMe, ev.Emoji) })
}

func (b *outbox) receipt(ev whatsapp.Receipt) bool {
	for _, id := range ev.IDs {
		if !b.move(ev.Chat, id, func() (bool, error) { return b.chats.Advance(ev.Chat, id, ev.State) }) {
			return false
		}
	}
	return true
}
