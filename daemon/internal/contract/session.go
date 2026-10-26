package contract

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// Clock is the one time seam of the Daemon: the time of the chat store, and the timers of the sweep, the connect retry and the Mute end.
// Tests replace it with a manual clock.
var Clock clock.Clock = clock.Real

// keepGoing is what a step returns when the Daemon goes on. Any other value is an exit code.
const keepGoing = -1

func exitIf(ok bool) int {
	if ok {
		return keepGoing
	}
	return ExitError
}

// session is the state of one Run. It lives on the Daemon loop; only the sends and downloads run on other goroutines.
// The fields below `ready` are nil until the Profile is open, and nil again after an unlink.
type session struct {
	profile   Profile
	prefs     *settings.Store
	newPort   NewPort
	desktop   desktop.Notifier
	send      func(map[string]any) bool
	logOut    io.Writer
	attn      attention.Attention // the pop-out state: it outlives the Profile, so a new link keeps it
	noWlPaste bool                // wl-paste is missing; the Daemon logged it once
	pasteBusy bool                // a clipboard read of draft.paste runs
	pasteDone chan pasteResult    // the results of the clipboard reads of draft.paste

	ready            bool
	port             whatsapp.Port
	media            *media.Store
	events           <-chan whatsapp.Event
	chats            *chatstore.Store
	box              *outbox
	read             *reader // clears Read Markers and sends Read Receipts
	notices          *notifier
	pres             *presenter
	loader           *mediaLoader
	hist             *history                  // the open On-Demand History Sync requests
	sent             <-chan sendResult         // the results of the sends
	mediaDone        <-chan mediaResult        // the results of the Media downloads
	participantsDone <-chan participantsResult // the results of the Group Chat Participant lookups
	historyEnded     <-chan historyEnd         // the requests of On-Demand History Sync that got no answer
	expiryTick       <-chan time.Time          // fires when the sweep of Disappearing Messages is due
	retryConnect     <-chan time.Time          // fires when a failed connect of a linked Profile is due again
	muteTick         <-chan time.Time          // fires when the next Mute ends, so the bar count moves
	muteAt           int64                     // the Mute end that muteTick waits for
}

// open opens the Profile and sends `ready`, or tells the Plugin that the keyring is unavailable.
// It returns an exit code, or keepGoing.
func (s *session) open() int {
	switch err := s.profile.Open(); {
	case err == nil:
		// The port exists before `ready`, so a test or the Plugin can rely on it from then on.
		var portErr error
		if s.port, portErr = s.newPort(); portErr != nil {
			fmt.Fprintln(s.logOut, "whatsapp client failed")
			return ExitError
		}
		s.events = s.port.Events()
		s.media = s.profile.Media()
		s.chats = chatstore.New(s.profile.Chat(), Clock.Now)
		s.read = &reader{chats: s.chats, port: s.port, prefs: s.prefs, attn: &s.attn, logOut: s.logOut, played: map[string]bool{}}
		s.notices = &notifier{chats: s.chats, prefs: s.prefs, attn: &s.attn, desktop: s.desktop, logOut: s.logOut}
		// A settings file that cannot be read gives the defaults.
		autoDownloads := func(size int64) bool {
			cur, err := s.prefs.Get()
			if err != nil {
				cur = settings.Defaults()
			}
			return cur.AutoDownloads(size)
		}
		s.box = newOutbox(s.chats, s.port, s.media, s.send, s.logOut, s.read.incoming, s.notices.show, autoDownloads)
		s.sent = s.box.results
		s.participantsDone = s.box.participants
		s.loader = newMediaLoader(s.chats, s.port, s.media, s.box.notify, s.logOut, autoDownloads)
		s.mediaDone = s.loader.results
		s.hist = newHistory(s.port, s.send, s.logOut)
		s.historyEnded = s.hist.ended
		// Messages that expired while the Daemon was off go before `ready`: the Plugin has shown none yet.
		if _, err := s.chats.Expire(Clock.Now().Unix()); err != nil {
			fmt.Fprintln(s.logOut, "chat store write failed")
		}
		s.expiryTick = Clock.After(ExpiryInterval)
		s.scheduleMuteEnd()
		s.pres = newPresenter(s.port, s.prefs, &s.attn, s.logOut)
		s.ready = true
		if !s.send(notification("ready", map[string]any{"keyringPlain": KeyringPlain()})) || !s.startPort() {
			return ExitError
		}
	case errors.Is(err, keystore.ErrUnavailable):
		fmt.Fprintln(s.logOut, "keyring unavailable")
		if !s.send(notification("keyringUnavailable", nil)) {
			return ExitError
		}
	default:
		fmt.Fprintln(s.logOut, "profile open failed") // the error text could name paths
		return ExitError
	}
	return keepGoing
}

// startPort runs after `ready`: a linked Profile says so, then the port connects.
func (s *session) startPort() bool {
	if s.port.Linked() && !s.send(notification("linked", nil)) {
		return false
	}
	return s.connect()
}

// connect connects the port. An unlinked Profile starts linking, and a failure ends it with `linkFailed`.
func (s *session) connect() bool {
	if err := s.port.Connect(); err != nil {
		fmt.Fprintln(s.logOut, "connect failed") // the error text could name hosts
		if !s.port.Linked() {
			return s.send(notification("linkFailed", map[string]any{"reason": whatsapp.ReasonConnectFailed}))
		}
		s.retryConnect = Clock.After(ConnectRetryDelay)
		return s.send(notification("disconnected", nil))
	}
	return true
}

// wipeMedia deletes the plain copies of Media, also when the Profile never opened (the store needs no key).
// A hard kill skips it. The Daemon then wipes at the next start (see main.go).
func (s *session) wipeMedia() { s.profile.Media().Wipe() }

// unlink runs the delete-the-Profile routine, then links again as on a fresh Profile.
// `by` is "user" for Log out. It is empty when the phone removed the Companion Device.
func (s *session) unlink(by string) int {
	s.port.Disconnect()
	s.wipeMedia()
	dropAllPasted() // the Drafts go with the Profile
	s.ready, s.port, s.media, s.events, s.chats, s.box, s.read, s.pres, s.loader, s.hist = false, nil, nil, nil, nil, nil, nil, nil, nil, nil
	s.sent, s.participantsDone, s.mediaDone, s.historyEnded, s.expiryTick, s.retryConnect, s.muteTick, s.muteAt = nil, nil, nil, nil, nil, nil, nil, 0
	s.attn.Set(s.attn.Open(), "") // the Chat is gone with the Profile
	if err := s.profile.Delete(); err != nil {
		fmt.Fprintln(s.logOut, "profile delete failed")
		return ExitError
	}
	params := map[string]any{}
	if by != "" {
		params["by"] = by
	}
	if !s.send(notification("unlinked", params)) {
		return ExitError
	}
	return s.open()
}

// sweepExpired deletes the Disappearing Messages whose timer ended and tells the Plugin. It returns false when a frame could not be written.
func (s *session) sweepExpired() bool {
	expired, err := s.chats.Expire(Clock.Now().Unix())
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
	}
	s.expiryTick = Clock.After(ExpiryInterval)
	for _, e := range expired {
		if !s.send(notification("messageExpired", map[string]any{"chat": e.Chat, "id": e.ID})) {
			return false
		}
	}
	return true
}

// onEvent turns a WhatsApp event into frames. Each kind of event is routed here once.
// It returns an exit code, or keepGoing.
func (s *session) onEvent(ev whatsapp.Event) int {
	ok := true
	switch ev := ev.(type) {
	case whatsapp.QRCode:
		uri, err := qrDataURI(ev.Code)
		if err != nil {
			fmt.Fprintln(s.logOut, "qr encode failed")
			ok = s.send(notification("linkFailed", map[string]any{"reason": whatsapp.ReasonUnexpected}))
		} else {
			ok = s.send(notification("linkQr", map[string]any{"qr": uri}))
		}
	case whatsapp.PairSuccess:
		ok = s.send(notification("linked", nil))
	case whatsapp.LinkFailed:
		ok = s.send(notification("linkFailed", map[string]any{"reason": ev.Reason}))
	case whatsapp.LoggedOut:
		return s.unlink("")
	case whatsapp.Connected:
		s.pres.connected() // before the `connected` frame
		ok = s.box.connected() && s.mergeKnownLIDs()
	case whatsapp.Disconnected:
		ok = s.box.disconnected() && s.hist.disconnected()
	case whatsapp.OwnDevices:
		ok = s.ownDevices(ev)
	case whatsapp.NewMessage:
		var isNew bool
		ok, isNew = s.box.incomingMessage(ev)
		if isNew {
			s.loader.autoStart(ev)
		}
	case whatsapp.MessageEdited:
		ok = s.box.edited(ev)
	case whatsapp.MessageRevoked:
		ok = s.box.revoked(ev)
	case whatsapp.Reaction:
		ok = s.box.reacted(ev)
	case whatsapp.Receipt:
		ok = s.box.receipt(ev)
	case whatsapp.DeletedForMe:
		ok = s.deletedForMe(ev)
	case whatsapp.ChatCleared:
		ok = s.chatCleared(ev)
	case whatsapp.ChatDeleted:
		ok = s.chatDeleted(ev)
	case whatsapp.ChatAlias:
		ok = s.merge(ev)
	case whatsapp.PictureChanged:
		ok = s.pictureChanged(ev.JID)
	case whatsapp.Presence:
		ok = s.send(notification("presenceChanged", map[string]any{"chat": ev.JID, "online": ev.Online, "lastSeen": ev.LastSeen}))
	case whatsapp.ChatPresence:
		name, err := s.chats.ParticipantName(ev.Sender)
		if err != nil {
			fmt.Fprintln(s.logOut, "chat store read failed")
			break
		}
		ok = s.send(notification("activityChanged", map[string]any{"chat": ev.Chat, "participant": ev.Sender, "name": name, "state": ev.State}))
	case whatsapp.HistorySync:
		added, err := s.chats.ApplyHistory(ev)
		if err != nil {
			fmt.Fprintln(s.logOut, "chat store write failed") // the error text could hold names
			break
		}
		ok = s.send(notification("chatsChanged", nil))
		s.scheduleMuteEnd()
		if ev.OnDemand {
			ok = ok && s.hist.answered(ev, added)
		}
	default: // App State: the chat store knows its kinds
		stored, err := s.chats.Apply(ev)
		if err != nil {
			fmt.Fprintln(s.logOut, "chat store write failed") // the error text could hold names
		} else if stored {
			ok = s.send(notification("chatsChanged", nil))
			s.scheduleMuteEnd()
		}
	}
	return exitIf(ok)
}

// mergeKnownLIDs merges every stored LID whose phone JID the LID store learned since, such as a group author
// whose Messages arrived before the mapping. It runs on each connect.
func (s *session) mergeKnownLIDs() bool {
	lids, err := s.chats.LIDs()
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store read failed")
		return true
	}
	for _, lid := range lids {
		if phone := s.port.PhoneOf(lid); phone != "" && !s.merge(whatsapp.ChatAlias{LID: lid, Phone: phone}) {
			return false
		}
	}
	return true
}

// merge folds the LID Chat of an alias into the phone Chat. The Plugin hears of it when something merged.
func (s *session) merge(a whatsapp.ChatAlias) bool {
	ids, idsErr := s.chats.MediaIDs(a.LID) // before the merge moves the rows
	changed, err := s.chats.MergeChat(a.LID, a.Phone)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	if !changed {
		return true
	}
	if idsErr != nil || s.media.MoveChat(a.LID, a.Phone, ids) != nil {
		fmt.Fprintln(s.logOut, "media move failed")
	}
	s.box.chatMerged(a.LID, a.Phone)
	if s.attn.Attended() == a.LID {
		s.attn.Set(true, a.Phone)
	}
	s.scheduleMuteEnd()
	return s.send(notification("chatMerged", map[string]any{"from": a.LID, "to": a.Phone})) && s.send(notification("chatsChanged", nil))
}

// scheduleMuteEnd sets one timer for the earliest Mute end, so the Plugin counts the Chat again when its Mute ends.
// It keeps the timer that waits for the same end, and drops an older one.
func (s *session) scheduleMuteEnd() {
	until, ok, err := s.chats.NextMuteEnd()
	switch {
	case err != nil:
		fmt.Fprintln(s.logOut, "chat store read failed")
	case !ok:
		s.muteTick, s.muteAt = nil, 0
	case until != s.muteAt:
		s.muteTick, s.muteAt = Clock.After(time.Duration(until-Clock.Now().Unix())*time.Second), until
	}
}

// ownDevices shows a notice when the account of the user has a device that no earlier list held.
// The set is updated first: a failed notice is not shown again after the next connection.
func (s *session) ownDevices(ev whatsapp.OwnDevices) bool {
	if ev.Failed {
		fmt.Fprintln(s.logOut, "own device list failed") // the error text could hold ids
		return true
	}
	added, err := s.chats.NoteOwnDevices(ev.IDs)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat store write failed")
		return true
	}
	return !added || s.box.systemMessage(whatsapp.NewMessage{Chat: ev.Chat, Kind: whatsapp.KindSelf, Message: ev.Notice})
}
