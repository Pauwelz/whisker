package contract

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ExpiryInterval is how often the Daemon deletes Disappearing Messages whose timer ended.
// shortcut: an expired Message can show for up to one interval, upgrade to a timer for the next deadline if that matters.
const ExpiryInterval = time.Minute

// ConnectRetryDelay is the wait before the Daemon connects again after a failed connect.
const ConnectRetryDelay = 5 * time.Second

// sendPort is what the outbox needs from WhatsApp.
type sendPort interface {
	Send(chat, id, text string, quote *whatsapp.Quote, timer int64) error
	SendMedia(chat, id string, m whatsapp.Media, plain *os.File, caption string, timer int64) error
	GroupParticipants(chat string) ([]whatsapp.Participant, error)
	IsGroupAdmin(chat string, users ...string) (bool, error)
	PhoneOf(lid string) string
}

// messageKey names a Message across Chats, for the maps that track work per Message.
func messageKey(chat, id string) string { return chat + "\x00" + id }

// outbox sends the Messages of the user and follows the connection.
// A Message waits as `pending` in the chat store until the connection is up, so a restart loses nothing.
// A send and its retries use one Message ID, so WhatsApp drops a repeat: a Message never shows twice.
// It runs on the Daemon loop only. Only the sends run on other goroutines.
type outbox struct {
	chats         *chatstore.Store
	port          sendPort
	send          func(map[string]any) bool
	logOut        io.Writer
	online        bool
	gen           int // counts the Connected events, so a send can tell that a reconnect came while it ran
	inflight      map[string]bool
	merged        map[string]string // LID Chat to phone Chat: a send that began in the LID Chat ends in the phone Chat
	results       chan sendResult
	participants  chan participantsResult   // the results of the Group Chat Participant lookups
	looking       map[string]bool           // Messages whose Participant lookup runs
	adminRevokes  chan adminRevokeResult    // the results of the admin checks of admin revokes
	store         *media.Store              // holds the files of Media to send
	incoming      func(whatsapp.NewMessage) // runs after a new Message is stored
	notice        func(whatsapp.NewMessage) // shows the desktop notification of a new Message, if the policy says so
	autoDownloads func(size int64) bool     // settings.Settings.AutoDownloads of the current settings
}

type sendResult struct {
	chat, id string
	gen      int
	err      error
}

type participantsResult struct {
	chat, id     string
	participants []whatsapp.Participant
	err          error
}

type adminRevokeResult struct {
	ev    whatsapp.MessageRevoked
	admin bool
	err   error
}

func newOutbox(chats *chatstore.Store, port sendPort, store *media.Store, send func(map[string]any) bool, logOut io.Writer,
	incoming func(whatsapp.NewMessage), notice func(whatsapp.NewMessage), autoDownloads func(size int64) bool) *outbox {
	return &outbox{chats: chats, port: port, store: store, send: send, logOut: logOut, incoming: incoming, notice: notice, autoDownloads: autoDownloads,
		inflight: map[string]bool{}, merged: map[string]string{}, results: make(chan sendResult, 64),
		participants: make(chan participantsResult, 64), looking: map[string]bool{}, adminRevokes: make(chan adminRevokeResult, 64)}
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

// sendMedia decrypts the stored file to its own plain copy for the upload and removes the copy after.
func (b *outbox) sendMedia(m chatstore.Message, ref whatsapp.Media, timer int64) error {
	path, err := b.store.UploadCopy(m.Chat, m.ID, ref.Kind)
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
	storeFailed(b.logOut, storeRead, err)
	for _, m := range pending {
		b.submit(m)
	}
}

// chatMerged records that the LID Chat is now the phone Chat, for the sends that began in it.
func (b *outbox) chatMerged(lid, phone string) { b.merged[lid] = phone }

// done handles the end of a send. It returns false when a frame could not be written.
func (b *outbox) done(r sendResult) bool {
	if to, ok := b.merged[r.chat]; ok {
		r.chat = to
	}
	delete(b.inflight, r.id)
	switch {
	case r.err == nil:
		ok := b.move(r.chat, r.id, func() (bool, error) { return b.chats.Advance(r.chat, r.id, whatsapp.StateSent) })
		if b.isGroup(r.chat) {
			b.freeze(r.chat, r.id)
		}
		return ok
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
	if storeFailed(b.logOut, storeWrite, err) {
		return true
	}
	return !changed || b.notify(chat, id)
}

// notify sends `messageChanged` for the stored Message.
func (b *outbox) notify(chat, id string) bool {
	m, err := b.chats.Message(chat, id)
	if storeFailed(b.logOut, storeRead, err) {
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

func (b *outbox) disconnected(reason string) bool {
	b.online = false
	var params map[string]any
	if reason != "" {
		params = map[string]any{"reason": reason}
	}
	return b.send(notification("disconnected", params))
}

// incomingMessage stores a new Message and tells the Plugin: the Message, the Chat list, and the desktop notification if the policy says so.
// It reports whether the Message is new: a stored Message, or one with the ID of another author's Message, tells nobody.
// A copy that fills an empty thumbnail sends `messageChanged` only.
func (b *outbox) incomingMessage(ev whatsapp.NewMessage) (ok, isNew bool) {
	if ev.Message.System != nil {
		return b.systemMessage(ev), false
	}
	isNew, thumbFilled, err := b.chats.PutLive(ev, livePolicy(ev, b.autoDownloads))
	if storeFailed(b.logOut, storeWrite, err) {
		return true, false
	}
	if !isNew {
		return !thumbFilled || b.notify(ev.Chat, ev.Message.ID), false // the open Chat shows the thumbnail; no notice, no Chat list change
	}
	b.incoming(ev)
	if ev.Message.FromMe && ev.Kind == whatsapp.KindGroup { // sent from another device of the user
		b.freeze(ev.Chat, ev.Message.ID)
	}
	if !b.notify(ev.Chat, ev.Message.ID) {
		return false, true
	}
	b.notice(ev) // before `chatsChanged`, so a test that read the frame has seen the notification
	return b.send(notification("chatsChanged", nil)), true
}

// systemMessage stores a System Message and tells the Plugin. It raises no notification and no Read Receipt.
// The Chat list hears of it too: a new subject renames the Chat.
func (b *outbox) systemMessage(ev whatsapp.NewMessage) bool {
	stored, err := b.chats.PutSystem(ev)
	if storeFailed(b.logOut, storeWrite, err) {
		return true
	}
	if !stored {
		return true
	}
	return b.notify(ev.Chat, ev.Message.ID) && b.send(notification("chatsChanged", nil))
}

func (b *outbox) edited(ev whatsapp.MessageEdited) bool {
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.Edit(ev, EditWindow) })
}

func (b *outbox) revoked(ev whatsapp.MessageRevoked) bool {
	if ev.Admin != "" {
		b.checkAdmin(ev)
		return true
	}
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.Revoke(ev.Chat, ev.ID, ev.Sender, ev.FromMe) })
}

// checkAdmin starts the live admin check of an admin revoke. The lookup runs off the Daemon loop.
// Only a revoke in a Group Chat can be an admin revoke. WhatsApp's say is trusted, nothing is cached.
func (b *outbox) checkAdmin(ev whatsapp.MessageRevoked) {
	if !b.isGroup(ev.Chat) {
		return
	}
	go func() {
		admin, err := b.port.IsGroupAdmin(ev.Chat, ev.Admin, ev.AdminAlt)
		b.adminRevokes <- adminRevokeResult{ev, admin, err}
	}()
}

// adminRevokeDone applies an admin revoke when the check found an admin. A failed check keeps the Message.
func (b *outbox) adminRevokeDone(r adminRevokeResult) bool {
	if r.err != nil {
		fmt.Fprintln(b.logOut, "group lookup failed")
		return true
	}
	if !r.admin {
		return true
	}
	return b.move(r.ev.Chat, r.ev.ID, func() (bool, error) { return b.chats.RevokeByAdmin(r.ev.Chat, r.ev.ID, r.ev.Sender, r.ev.FromMe) })
}

func (b *outbox) reacted(ev whatsapp.Reaction) bool {
	return b.move(ev.Chat, ev.ID, func() (bool, error) { return b.chats.SetReaction(ev.Chat, ev.ID, ev.Sender, ev.FromMe, ev.Emoji) })
}

func (b *outbox) receipt(ev whatsapp.Receipt) bool {
	for _, id := range ev.IDs {
		if b.isGroup(ev.Chat) {
			if !b.groupReceipt(ev, id) {
				return false
			}
			continue
		}
		if !b.move(ev.Chat, id, func() (bool, error) { return b.chats.Advance(ev.Chat, id, ev.State) }) {
			return false
		}
	}
	return true
}

// isGroup tells that a stored Chat is a Group Chat.
func (b *outbox) isGroup(chat string) bool {
	c, err := b.chats.Chat(chat)
	return err == nil && c.Kind == whatsapp.KindGroup
}

// groupReceipt keeps a receipt of a Group Chat and moves the Message to the state that every Recipient reached.
// The receipt counts for the sender ID and for the phone JID of a LID, so it matches a Recipient by either ID.
// Without the set, it tries the Participant lookup again: a failed lookup has no timer.
func (b *outbox) groupReceipt(ev whatsapp.Receipt, id string) bool {
	tracked := false
	for _, sender := range []string{ev.Sender, b.port.PhoneOf(ev.Sender)} {
		if sender == "" {
			continue
		}
		t, err := b.chats.RecordReceipt(ev.Chat, id, sender, ev.State)
		if storeFailed(b.logOut, storeWrite, err) {
			return true
		}
		tracked = tracked || t
	}
	if !tracked {
		return true
	}
	ok, known := b.advanceGroup(ev.Chat, id)
	if ok && !known {
		b.freeze(ev.Chat, id)
	}
	return ok
}

// advanceGroup moves a Message of a Group Chat to the state that every Recipient reached.
// known is false while the set is not frozen.
func (b *outbox) advanceGroup(chat, id string) (ok, known bool) {
	state, known, err := b.chats.Combined(chat, id)
	if storeFailed(b.logOut, storeRead, err) {
		return true, true
	}
	if !known || state == "" {
		return true, known
	}
	return b.move(chat, id, func() (bool, error) { return b.chats.Advance(chat, id, state) }), true
}

// freeze starts the lookup of the Recipients of a Message in a Group Chat. The lookup runs off the Daemon loop.
func (b *outbox) freeze(chat, id string) {
	key := messageKey(chat, id)
	if b.looking[key] {
		return
	}
	b.looking[key] = true
	go func() {
		participants, err := b.port.GroupParticipants(chat)
		b.participants <- participantsResult{chat, id, participants, err}
	}()
}

// participantsDone stores the Recipients of a Message, then counts the receipts that came before them.
func (b *outbox) participantsDone(r participantsResult) bool {
	delete(b.looking, messageKey(r.chat, r.id))
	if r.err != nil {
		fmt.Fprintln(b.logOut, "group lookup failed") // the next receipt of the Message tries again
		return true
	}
	for i, p := range r.participants { // a receipt can name the other ID than GetGroupInfo gave
		if p.Phone == "" && p.LID != "" {
			r.participants[i].Phone = b.port.PhoneOf(p.LID)
		}
	}
	if err := b.chats.SetRecipients(r.chat, r.id, r.participants); storeFailed(b.logOut, storeWrite, err) {
		return true
	}
	ok, _ := b.advanceGroup(r.chat, r.id)
	return ok
}
