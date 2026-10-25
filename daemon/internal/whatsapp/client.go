package whatsapp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

// pairTimeout bounds the PairPhone network call. sendTimeout bounds one Send.
// editTimeout bounds one Edit, Revoke or App State write, which block the Daemon loop.
// storeTimeout bounds one call to the LID store.
const (
	pairTimeout = 30 * time.Second
	sendTimeout = 90 * time.Second
	// shortcut: one fixed limit for every download, upgrade to a size based limit if a large video times out.
	downloadTimeout = 10 * time.Minute
	editTimeout     = 15 * time.Second
	uploadTimeout   = 10 * time.Minute
	storeTimeout    = 5 * time.Second
)

// Client is the whatsmeow adapter: the only place whatsmeow types appear.
// Tests never use it: it would reach the WhatsApp servers.
type Client struct {
	cli    *whatsmeow.Client
	events chan Event
	mu     sync.Mutex // one Connect at a time

	syncMu  sync.Mutex
	pending []Event // the flag events of a full App State sync, until it completes
}

// SetDeviceIdentity names the linked device "whisker (hostname)" on the phone. Call it once, before the first connect and pair.
// WhatsApp keeps the name it got at pairing. A version that is not "major.minor.patch" at its start counts as 0.0.0.
func SetDeviceIdentity(version string) {
	var v [3]uint32
	fmt.Sscanf(version, "%d.%d.%d", &v[0], &v[1], &v[2])
	host, _ := os.Hostname()
	store.SetOSInfo(deviceName(host), v)
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
}

// deviceName is "whisker (host)", or "whisker" when the hostname is unknown.
func deviceName(host string) string {
	if host == "" {
		return "whisker"
	}
	return "whisker (" + host + ")"
}

// NewClient loads the device from the device store (a new unpaired device if there is none).
func NewClient(container *sqlstore.Container) (*Client, error) {
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop), events: make(chan Event, 64)}
	// The first App State sync after a link is a full sync. Without this, its Pin, Archive, Mute and Read Marker events are lost.
	c.cli.EmitAppStateEventsOnFullSync = true
	c.cli.AddEventHandler(c.handle)
	return c, nil
}

func (c *Client) Linked() bool         { return c.cli.Store.ID != nil }
func (c *Client) Events() <-chan Event { return c.events }
func (c *Client) OwnUsers() []string {
	if c.cli.Store.ID == nil {
		return nil
	}
	return []string{c.cli.Store.GetJID().User, c.cli.Store.GetLID().User}
}

// isOwn tells that a JID is a device of the user, by phone JID or LID.
func (c *Client) isOwn(jid types.JID) bool {
	return jid.User != "" && slices.Contains(c.OwnUsers(), jid.User)
}

func (c *Client) GroupMembers(chat string) ([]Member, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	if !c.cli.IsConnected() {
		return nil, ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	info, err := c.cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, offline(err)
	}
	var members []Member
	for _, p := range info.Participants {
		m := Member{Phone: p.PhoneNumber.ToNonAD().String(), LID: p.LID.ToNonAD().String()}
		// The primary JID is one of the two IDs: fill the one that the fields leave empty.
		switch p.JID.Server {
		case types.DefaultUserServer:
			m.Phone = p.JID.ToNonAD().String()
		case types.HiddenUserServer:
			m.LID = p.JID.ToNonAD().String()
		}
		if !c.isOwn(p.JID) && !c.isOwn(p.PhoneNumber) && !c.isOwn(p.LID) {
			members = append(members, m)
		}
	}
	return members, nil
}

func (c *Client) Logout() error {
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	return c.cli.Logout(ctx)
}
func (c *Client) Disconnect() { c.cli.Disconnect() }

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cli.Disconnect() // a retry starts from a closed socket
	if c.Linked() {
		return c.cli.Connect()
	}
	qrs, err := c.cli.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	if err := c.cli.Connect(); err != nil {
		return err
	}
	go c.forwardQRs(qrs)
	return nil
}

func (c *Client) forwardQRs(qrs <-chan whatsmeow.QRChannelItem) {
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			c.events <- QRCode{Code: item.Code}
		case whatsmeow.QRChannelSuccess.Event:
			// events.PairSuccess reports success.
		default:
			c.events <- LinkFailed{Reason: qrFailureReason(item)}
		}
	}
}

func qrFailureReason(item whatsmeow.QRChannelItem) string {
	switch item {
	case whatsmeow.QRChannelTimeout:
		return ReasonTimeout
	case whatsmeow.QRChannelClientOutdated:
		return ReasonClientOutdated
	case whatsmeow.QRChannelScannedWithoutMultidevice:
		return ReasonScannedWithoutMultidevice
	}
	return ReasonUnexpected
}

func (c *Client) PairCode(phone string) (string, error) {
	digits, err := cleanPhone(phone)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	return c.cli.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
}

func (c *Client) PhoneOf(lid string) string {
	jid, err := types.ParseJID(lid)
	if err != nil || jid.Server != types.HiddenUserServer {
		return ""
	}
	pn, err := c.lookupPhone(jid)
	if err != nil || pn.IsEmpty() {
		return ""
	}
	return pn.ToNonAD().String()
}

func (c *Client) Lookup(phone string) (string, string, error) {
	digits, err := cleanPhone(phone)
	if err != nil {
		return "", "", err
	}
	if !c.cli.IsConnected() {
		return digits, "", ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	found, err := c.cli.IsOnWhatsApp(ctx, []string{digits})
	if err != nil {
		return digits, "", offline(err)
	}
	for _, r := range found {
		if r.IsIn && r.Query == digits {
			if !r.PhoneNumber.IsEmpty() { // a LID answer still names the phone ID, the key of the Chat
				return digits, r.PhoneNumber.String(), nil
			}
			return digits, r.JID.String(), nil
		}
	}
	return digits, "", nil
}

// maxPictureBytes caps the preview download. A preview is a few KiB, so a larger body is not one.
const maxPictureBytes = 512 << 10

func (c *Client) ProfilePicture(chat, knownID string) (string, []byte, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return "", nil, err
	}
	if !c.cli.IsConnected() {
		return "", nil, ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	info, err := c.cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true, ExistingID: knownID})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		return "", nil, nil
	case err != nil:
		return "", nil, offline(err)
	case info == nil: // the known picture is still current
		return knownID, nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, errors.New("picture download failed") // the error text holds the URL
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, errors.New("picture download failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPictureBytes+1))
	if err != nil || len(data) > maxPictureBytes {
		return "", nil, errors.New("picture download failed")
	}
	return info.ID, data, nil
}

func (c *Client) NewMessageID() string { return c.cli.GenerateMessageID() }

func (c *Client) Send(chat, id, text string, quote *Quote, timer int64) error {
	return c.send(chat, sendTimeout, func(jid types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra) {
		msg := &waE2E.Message{Conversation: proto.String(text)}
		if quote != nil || timer > 0 { // a quote and the timer ride in the ContextInfo of an extended text Message
			info := &waE2E.ContextInfo{}
			if quote != nil {
				info.StanzaID = proto.String(quote.ID)
				info.Participant = proto.String(c.quoteSender(quote.Sender))
				info.QuotedMessage = &waE2E.Message{Conversation: proto.String(quote.Text)}
			}
			if timer > 0 {
				info.Expiration = proto.Uint32(uint32(timer))
			}
			msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: info}}
		}
		return msg, whatsmeow.SendRequestExtra{ID: id}
	})
}

// quoteSender is the JID that WhatsApp expects for the author of a quoted Message. The Quote holds none for the user's own Messages.
func (c *Client) quoteSender(sender string) string {
	if sender == "" && c.cli.Store.ID != nil {
		return c.cli.Store.ID.ToNonAD().String()
	}
	return sender
}

func (c *Client) React(chat, id, sender, emoji string) error {
	senderJID := types.EmptyJID
	if sender != "" {
		var err error
		if senderJID, err = types.ParseJID(sender); err != nil {
			return err
		}
	}
	return c.send(chat, editTimeout, func(jid types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra) {
		return c.cli.BuildReaction(jid, senderJID, id, emoji), whatsmeow.SendRequestExtra{}
	})
}

// SendMedia uploads plain, then sends the Media Message. A retry uses the same Message ID.
func (c *Client) SendMedia(chat, id string, m Media, plain *os.File, caption string, timer int64) error {
	if !c.cli.IsConnected() {
		return ErrOffline
	}
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	up, err := c.cli.UploadReader(ctx, plain, nil, whatsmeow.MediaType(m.Kind.Info()))
	if err != nil {
		return err
	}
	msg := &waE2E.Message{}
	if m.Kind == media.Image {
		msg.ImageMessage = &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(m.Mime), Caption: proto.String(caption),
		}
	} else {
		msg.DocumentMessage = &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(m.Mime), FileName: proto.String(m.Name), Caption: proto.String(caption),
		}
	}
	if timer > 0 {
		info := &waE2E.ContextInfo{Expiration: proto.Uint32(uint32(timer))}
		if msg.ImageMessage != nil {
			msg.ImageMessage.ContextInfo = info
		} else {
			msg.DocumentMessage.ContextInfo = info
		}
	}
	return c.send(chat, sendTimeout, func(types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra) {
		return msg, whatsmeow.SendRequestExtra{ID: id}
	})
}

func (c *Client) Edit(chat, id, text string) error {
	return c.send(chat, editTimeout, func(jid types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra) {
		return c.cli.BuildEdit(jid, id, &waE2E.Message{Conversation: proto.String(text)}), whatsmeow.SendRequestExtra{}
	})
}

func (c *Client) Revoke(chat, id string) error {
	return c.send(chat, editTimeout, func(jid types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra) {
		return c.cli.BuildRevoke(jid, types.EmptyJID, id), whatsmeow.SendRequestExtra{}
	})
}

// send sends the Message that build makes and maps a missing connection to ErrOffline.
func (c *Client) send(chat string, timeout time.Duration, build func(types.JID) (*waE2E.Message, whatsmeow.SendRequestExtra)) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	msg, extra := build(jid)
	_, err = c.cli.SendMessage(ctx, jid, msg, extra)
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return ErrOffline
	}
	return err
}

func (c *Client) MarkRead(chat, sender string, ids []string) error {
	return c.receipt(chat, sender, ids)
}

func (c *Client) MarkPlayed(chat, sender, id string) error {
	return c.receipt(chat, sender, []string{id}, types.ReceiptTypePlayed)
}

func (c *Client) receipt(chat, sender string, ids []string, kind ...types.ReceiptType) error {
	chatJID, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	senderJID, err := types.ParseJID(sender)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	err = c.cli.MarkRead(ctx, ids, time.Now(), chatJID, senderJID, kind...)
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return ErrOffline
	}
	return err
}

func (c *Client) MarkChatRead(chat string) error {
	return c.appState(chat, func(jid types.JID) appstate.PatchInfo {
		return appstate.BuildMarkChatAsRead(jid, true, time.Now(), nil)
	})
}

func (c *Client) SetPinned(chat string, pinned bool) error {
	return c.appState(chat, func(jid types.JID) appstate.PatchInfo { return appstate.BuildPin(jid, pinned) })
}

func (c *Client) SetMuted(chat string, until int64) error {
	return c.appState(chat, func(jid types.JID) appstate.PatchInfo {
		switch {
		case until == 0:
			return appstate.BuildMuteAbs(jid, false, nil)
		case until < 0:
			return appstate.BuildMuteAbs(jid, true, nil) // no end time: for ever
		}
		end := until * 1000 // WhatsApp counts the end in milliseconds
		return appstate.BuildMuteAbs(jid, true, &end)
	})
}

func (c *Client) SetUnread(chat string, unread bool) error {
	return c.appState(chat, func(jid types.JID) appstate.PatchInfo {
		return appstate.BuildMarkChatAsRead(jid, !unread, time.Now(), nil)
	})
}

// deleteForMePatch is the App State patch that deletes a Message on the user's devices only.
// The index ends with the author: "0" for the user and for the other person of a Direct Chat.
func deleteForMePatch(chat types.JID, id, sender string, fromMe bool, ts int64) appstate.PatchInfo {
	own, author := "0", "0"
	if fromMe {
		own = "1"
	} else if jid, err := types.ParseJID(sender); err == nil && jid.User != chat.User {
		author = jid.String()
	}
	return appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexDeleteMessageForMe, chat.String(), id, own, author},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{DeleteMedia: proto.Bool(true), MessageTimestamp: proto.Int64(ts)},
			},
		}},
	}
}

func (c *Client) DeleteForMe(chat, id, sender string, fromMe bool, ts int64) error {
	return c.appState(chat, func(jid types.JID) appstate.PatchInfo { return deleteForMePatch(jid, id, sender, fromMe, ts) })
}

// appState sends the App State patch that build makes for a Chat.
// shortcut: the call blocks the Daemon loop (up to 15 s), upgrade when another request must run meanwhile.
func (c *Client) appState(chat string, build func(types.JID) appstate.PatchInfo) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	return offline(c.cli.SendAppState(ctx, build(jid)))
}

func (c *Client) SendPresence(online bool) error {
	state := types.PresenceUnavailable
	if online {
		state = types.PresenceAvailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	return offline(c.cli.SendPresence(ctx, state))
}

func (c *Client) SubscribePresence(jid string) error {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	return offline(c.cli.SubscribePresence(ctx, parsed))
}

func (c *Client) SendTyping(chat string, composing bool) error {
	parsed, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	state := types.ChatPresencePaused
	if composing {
		state = types.ChatPresenceComposing
	}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	return offline(c.cli.SendChatPresence(ctx, parsed, state, types.ChatPresenceMediaText))
}

// offline maps a missing connection to ErrOffline.
func offline(err error) error {
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return ErrOffline
	}
	return err
}

func (c *Client) handle(evt any) {
	switch evt := evt.(type) {
	case *events.Presence:
		p := Presence{JID: c.phone(evt.From).String(), Online: !evt.Unavailable}
		if !evt.LastSeen.IsZero() {
			p.LastSeen = evt.LastSeen.Unix()
		}
		c.events <- p
	case *events.ChatPresence:
		state := ActivityPaused
		if evt.State == types.ChatPresenceComposing {
			state = ActivityComposing
			if evt.Media == types.ChatPresenceMediaAudio {
				state = ActivityRecording
			}
		}
		c.events <- ChatPresence{Chat: c.chatOf(evt.MessageSource).String(), Sender: c.senderOf(evt.MessageSource).String(), State: state}
	case *events.Connected:
		c.events <- Connected{}
		c.emitContactBook()
	case *events.Disconnected:
		c.events <- Disconnected{}
	case *events.Message:
		c.emitLiveAlias(evt.Info.MessageSource)
		if c.handleProtocol(evt) {
			return
		}
		chat := c.chatOf(evt.Info.MessageSource)
		if r := evt.Message.GetReactionMessage(); r != nil {
			c.events <- Reaction{Chat: chat.String(), ID: r.GetKey().GetID(), Sender: c.senderOf(evt.Info.MessageSource).String(), FromMe: evt.Info.IsFromMe, Emoji: r.GetText()}
			return
		}
		body, ok := contentOf(evt.Message, evt.IsViewOnce)
		if !ok || evt.IsEdit || chat.Server == types.BroadcastServer {
			return
		}
		c.events <- NewMessage{Chat: chat.String(), Kind: c.kindOf(chat), Message: c.messageOf(evt.Info, evt.Message, body)}
	case *events.Receipt:
		state := receiptState(evt.Type)
		if state == "" || (evt.IsGroup && c.isOwn(evt.Sender)) {
			return
		}
		ids := make([]string, len(evt.MessageIDs))
		for i, id := range evt.MessageIDs {
			ids[i] = id
		}
		r := Receipt{Chat: c.chatOf(evt.MessageSource).String(), IDs: ids, State: state}
		if evt.IsGroup {
			r.Sender = evt.Sender.ToNonAD().String() // a phone JID or a LID: the chat store matches both
		}
		c.events <- r
	case *events.PairSuccess:
		c.events <- PairSuccess{}
	case *events.PairError:
		c.events <- LinkFailed{Reason: ReasonPairError}
	case *events.ClientOutdated:
		c.events <- LinkFailed{Reason: ReasonClientOutdated}
	case *events.LoggedOut:
		c.events <- LoggedOut{}
	case *events.HistorySync:
		batch, aliases := c.historySync(evt.Data)
		c.events <- batch
		for _, a := range aliases { // after the batch: a LID Chat of an earlier batch is in the store by then
			c.events <- a
		}
	case *events.PushName:
		c.events <- PushName{JID: c.phone(evt.JID).String(), Name: evt.NewPushName}
	case *events.AppStateSyncComplete:
		c.flushFullSync()
		c.emitContactBook()
	case *events.Contact:
		if !evt.FromFullSync { // a full sync ends with one ContactBook, not a frame per contact
			c.events <- ContactName{JID: c.phone(evt.JID).String(), Name: evt.Action.GetFullName()}
		}
	case *events.Picture:
		c.events <- PictureChanged{JID: c.phone(evt.JID).String()}
	case *events.Pin:
		c.emitFlag(Pin{JID: c.phone(evt.JID).String(), Pinned: evt.Action.GetPinned()}, evt.FromFullSync)
	case *events.Archive:
		c.emitFlag(Archive{JID: c.phone(evt.JID).String(), Archived: evt.Action.GetArchived()}, evt.FromFullSync)
	case *events.UnarchiveChatsSetting:
		c.emitFlag(UnarchiveChats{On: evt.Action.GetUnarchiveChats()}, evt.FromFullSync)
	case *events.Mute:
		until := int64(0)
		if evt.Action.GetMuted() {
			until = muteUntil(appStateMuteSeconds(evt.Action.GetMuteEndTimestamp()))
		}
		c.emitFlag(Mute{JID: c.phone(evt.JID).String(), Until: until}, evt.FromFullSync)
	case *events.MarkChatAsRead:
		c.emitFlag(ReadMarker{JID: c.phone(evt.JID).String(), Unread: !evt.Action.GetRead()}, evt.FromFullSync)
	}
}

// emitFlag sends a live flag event at once. A flag event of a full sync waits for the end of the sync,
// so a sync of hundreds of Chats costs the Plugin one refresh.
func (c *Client) emitFlag(ev Event, fullSync bool) {
	if !fullSync {
		c.events <- ev
		return
	}
	c.syncMu.Lock()
	c.pending = append(c.pending, ev)
	c.syncMu.Unlock()
}

// flushFullSync sends the waiting flag events of a full sync as one AppStateBatch.
func (c *Client) flushFullSync() {
	c.syncMu.Lock()
	batch := c.pending
	c.pending = nil
	c.syncMu.Unlock()
	if len(batch) > 0 {
		c.events <- AppStateBatch{Events: batch}
	}
}

// handleProtocol turns an edit or a revoke into its event. It reports whether the Message was one of them.
func (c *Client) handleProtocol(evt *events.Message) bool {
	pm := evt.Message.GetProtocolMessage()
	if pm == nil {
		return false // the zero type of a missing ProtocolMessage is REVOKE
	}
	chat, id := c.chatOf(evt.Info.MessageSource).String(), pm.GetKey().GetID()
	switch {
	case pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT:
		if text := messageText(pm.GetEditedMessage()); text != "" {
			c.events <- MessageEdited{Chat: chat, ID: id, Sender: c.senderOf(evt.Info.MessageSource).String(), FromMe: evt.Info.IsFromMe, Text: text}
		}
	case pm.GetType() == waE2E.ProtocolMessage_REVOKE:
		c.events <- MessageRevoked{Chat: chat, ID: id}
	case pm.GetType() == waE2E.ProtocolMessage_EPHEMERAL_SETTING:
		c.events <- ChatTimer{Chat: chat, Seconds: int64(pm.GetEphemeralExpiration())}
	default:
		return false
	}
	return true
}

// timerOf is the Disappearing timer in seconds that a Message carries in its ContextInfo, or 0.
func timerOf(m *waE2E.Message) int64 {
	var info *waE2E.ContextInfo
	switch {
	case m.GetExtendedTextMessage() != nil:
		info = m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		info = m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		info = m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		info = m.GetAudioMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		info = m.GetStickerMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		info = m.GetDocumentMessage().GetContextInfo()
	}
	return int64(info.GetExpiration())
}

func receiptState(t types.ReceiptType) string {
	switch t {
	case types.ReceiptTypeDelivered:
		return StateDelivered
	case types.ReceiptTypeRead:
		return StateRead
	case types.ReceiptTypePlayed:
		return StatePlayed
	}
	return ""
}

// historyState maps the status of a History Sync Message to a Delivery State, or "" for one before delivered.
func historyState(s waWeb.WebMessageInfo_Status) string {
	switch s {
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return StateDelivered
	case waWeb.WebMessageInfo_READ:
		return StateRead
	case waWeb.WebMessageInfo_PLAYED:
		return StatePlayed
	}
	return ""
}

// muteUntil maps a WhatsApp mute end (Unix seconds, -1 or 0 for ever) to the port value.
func muteUntil(end int64) int64 {
	if end <= 0 {
		return -1
	}
	return end
}

// appStateMuteSeconds converts a mute end of App State (Unix milliseconds, like whatsmeow's time.UnixMilli) to seconds.
// A negative value (for ever) stays negative.
func appStateMuteSeconds(ms int64) int64 {
	if ms < 0 {
		return ms
	}
	return ms / 1000
}

// aliasOf is the alias of a LID and a phone JID, or false when they are not such a pair.
func aliasOf(lid, phone types.JID) (ChatAlias, bool) {
	if lid.Server != types.HiddenUserServer || phone.Server != types.DefaultUserServer {
		return ChatAlias{}, false
	}
	return ChatAlias{LID: lid.ToNonAD().String(), Phone: phone.ToNonAD().String()}, true
}

// historyAliases are the LID to phone pairs of a History Sync batch: its mappings, then its conversations that carry a phone JID.
func historyAliases(data *waHistorySync.HistorySync) []ChatAlias {
	var out []ChatAlias
	for _, m := range data.GetPhoneNumberToLidMappings() {
		lid, err := types.ParseJID(m.GetLidJID())
		if err != nil {
			continue
		}
		if a, ok := aliasOf(lid, phoneOfConversation(m.GetPnJID())); ok {
			out = append(out, a)
		}
	}
	for _, conv := range data.GetConversations() {
		if lid, err := types.ParseJID(conv.GetID()); err == nil {
			if a, ok := aliasOf(lid, phoneOfConversation(conv.GetPnJID())); ok {
				out = append(out, a)
			}
		}
	}
	return out
}

// emitContactBook sends the contact names of whatsmeow's store. A failed read only delays the names to the next connect.
func (c *Client) emitContactBook() {
	if c.cli.Store.Contacts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	contacts, err := c.cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}
	if book := contactBook(contacts, func(j types.JID) types.JID { return c.phone(j) }); len(book.Names) > 0 {
		c.events <- book
	}
}

// contactBook keeps the contacts that have a full name, under their phone JID, sorted by JID.
func contactBook(contacts map[types.JID]types.ContactInfo, phone func(types.JID) types.JID) ContactBook {
	var book ContactBook
	for jid, info := range contacts {
		if info.FullName != "" {
			book.Names = append(book.Names, ContactName{JID: phone(jid).String(), Name: info.FullName})
		}
	}
	slices.SortFunc(book.Names, func(a, b ContactName) int { return strings.Compare(a.JID, b.JID) })
	return book
}

// emitLiveAlias tells the chat store that the LID of a live event and the phone JID it resolved to are one Chat.
func (c *Client) emitLiveAlias(src types.MessageSource) {
	for _, a := range liveAliases(src, c.chatOf(src)) {
		c.events <- a
	}
}

// liveAliases are the LID to phone pairs that a live event teaches: its Chat, given its resolved phone JID, and its sender and sender alt in either order.
// The sender pair lets group Messages stored under a LID take the name of the phone JID.
func liveAliases(src types.MessageSource, chat types.JID) []ChatAlias {
	var out []ChatAlias
	for _, pair := range [][2]types.JID{{src.Chat, chat}, {src.Sender, src.SenderAlt}, {src.SenderAlt, src.Sender}} {
		if a, ok := aliasOf(pair[0], pair[1]); ok && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// historySync maps one History Sync batch, and lists the aliases that the batch taught. Chats that do not parse are dropped.
func (c *Client) historySync(data *waHistorySync.HistorySync) (HistorySync, []ChatAlias) {
	batch := HistorySync{}
	for _, p := range data.GetPushnames() {
		batch.PushNames = append(batch.PushNames, PushName{JID: c.phoneString(p.GetID()), Name: p.GetPushname()})
	}
	for _, conv := range data.GetConversations() {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		pn := phoneOfConversation(conv.GetPnJID())
		if jid.Server == types.HiddenUserServer && !pn.IsEmpty() {
			c.rememberPhone(jid, pn)
		}
		jid = c.phone(jid, pn)
		chat := HistoryChat{
			JID:        jid.String(),
			Kind:       c.kindOf(jid),
			LastActive: int64(max(conv.GetConversationTimestamp(), conv.GetLastMsgTimestamp())),
			Pinned:     conv.GetPinned() != 0,
			Archived:   conv.GetArchived(),
			Unread:     conv.GetUnreadCount() > 0 || conv.GetMarkedAsUnread(),
			Timer:      int64(conv.GetEphemeralExpiration()),
		}
		if chat.Kind == KindGroup {
			chat.Subject = conv.GetName()
		}
		if conv.GetMuteEndTime() != 0 {
			chat.MutedUntil = muteUntil(int64(conv.GetMuteEndTime()))
		}
		for _, hm := range conv.GetMessages() {
			parsed, err := c.cli.ParseWebMessage(jid, hm.GetMessage())
			if err != nil {
				continue
			}
			if body, ok := contentOf(parsed.Message, parsed.IsViewOnce); ok {
				m := c.messageOf(parsed.Info, parsed.Message, body)
				m.Reactions = historyReactions(hm.GetMessage().GetReactions(), c.phoneString)
				if m.FromMe {
					m.State = historyState(hm.GetMessage().GetStatus())
				}
				chat.Messages = append(chat.Messages, m)
			}
		}
		batch.Chats = append(batch.Chats, chat)
	}
	return batch, historyAliases(data)
}

// historyReactions maps the Reactions that History Sync carries on a Message. The reactor is the participant of the
// Reaction key in a Group Chat, else the Chat; the user's own Reaction has no sender. An empty emoji is a removed Reaction.
func historyReactions(rs []*waWeb.Reaction, phone func(string) string) []HistoryReaction {
	var out []HistoryReaction
	for _, r := range rs {
		if r.GetText() == "" {
			continue
		}
		key := r.GetKey()
		if key.GetFromMe() {
			out = append(out, HistoryReaction{FromMe: true, Emoji: r.GetText()})
			continue
		}
		out = append(out, HistoryReaction{Sender: phone(cmp.Or(key.GetParticipant(), key.GetRemoteJID())), Emoji: r.GetText()})
	}
	return out
}

func (c *Client) messageOf(info types.MessageInfo, m *waE2E.Message, body content) Message {
	return Message{
		ID: info.ID, Sender: c.senderOf(info.MessageSource).String(), FromMe: info.IsFromMe, Time: info.Timestamp.Unix(),
		Text: body.Text, PushName: info.PushName, Quote: c.quoteOf(m), Media: body.Media, Timer: timerOf(m),
		Mentions: c.phoneStrings(body.Mentions), MentionsMe: mentionsUser(body.Mentions, c.cli.Store.GetJID().User, c.cli.Store.GetLID().User),
	}
}

func (c *Client) kindOf(jid types.JID) string {
	switch {
	case jid.Server == types.GroupServer:
		return KindGroup
	case c.cli.Store.ID != nil && jid.User == c.cli.Store.ID.User:
		return KindSelf
	}
	return KindDirect
}

// quoteOf is the Quote that a Message carries, or nil. The user's own JID maps to the empty sender.
func (c *Client) quoteOf(m *waE2E.Message) *Quote {
	ctx := m.GetExtendedTextMessage().GetContextInfo()
	if ctx.GetStanzaID() == "" {
		return nil
	}
	sender := c.phoneString(ctx.GetParticipant())
	if jid, err := types.ParseJID(sender); err == nil && c.cli.Store.ID != nil && jid.User == c.cli.Store.ID.User {
		sender = ""
	}
	return &Quote{ID: ctx.GetStanzaID(), Sender: sender, Text: messageText(ctx.GetQuotedMessage())}
}

// messageText is the text of a Message, or the caption of its Media. System Messages have none yet.
func messageText(m *waE2E.Message) string {
	if text := m.GetConversation(); text != "" {
		return text
	}
	if text := m.GetExtendedTextMessage().GetText(); text != "" {
		return text
	}
	switch {
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption()
	}
	return m.GetDocumentMessage().GetCaption()
}

// mediaOf maps the Media of a Message to the port type, or nil when the Message has none.
func mediaOf(m *waE2E.Message) *Media {
	switch {
	case m.GetImageMessage() != nil:
		im := m.GetImageMessage()
		return &Media{Kind: media.Image, Mime: im.GetMimetype(), Size: int64(im.GetFileLength()), Key: im.GetMediaKey(), DirectPath: im.GetDirectPath(), FileSHA256: im.GetFileSHA256(), FileEncSHA256: im.GetFileEncSHA256(), Thumbnail: im.GetJPEGThumbnail(),
			ThumbDirectPath: im.GetThumbnailDirectPath(), ThumbSHA256: im.GetThumbnailSHA256(), ThumbEncSHA256: im.GetThumbnailEncSHA256()}
	case m.GetStickerMessage() != nil:
		sm := m.GetStickerMessage()
		return &Media{Kind: media.Sticker, Mime: sm.GetMimetype(), Size: int64(sm.GetFileLength()), Key: sm.GetMediaKey(), DirectPath: sm.GetDirectPath(), FileSHA256: sm.GetFileSHA256(), FileEncSHA256: sm.GetFileEncSHA256()}
	case m.GetAudioMessage() != nil:
		am := m.GetAudioMessage()
		kind := media.Audio
		if am.GetPTT() {
			kind = media.Voice
		}
		return &Media{Kind: kind, Mime: am.GetMimetype(), Size: int64(am.GetFileLength()), Key: am.GetMediaKey(), DirectPath: am.GetDirectPath(), FileSHA256: am.GetFileSHA256(), FileEncSHA256: am.GetFileEncSHA256(), Duration: int64(am.GetSeconds())}
	case m.GetVideoMessage() != nil:
		vm := m.GetVideoMessage()
		return &Media{Kind: media.Video, Mime: vm.GetMimetype(), Size: int64(vm.GetFileLength()), Key: vm.GetMediaKey(), DirectPath: vm.GetDirectPath(), FileSHA256: vm.GetFileSHA256(), FileEncSHA256: vm.GetFileEncSHA256(), Thumbnail: vm.GetJPEGThumbnail(), Duration: int64(vm.GetSeconds()), Gif: vm.GetGifPlayback(),
			ThumbDirectPath: vm.GetThumbnailDirectPath(), ThumbSHA256: vm.GetThumbnailSHA256(), ThumbEncSHA256: vm.GetThumbnailEncSHA256()}
	case m.GetDocumentMessage() != nil:
		dm := m.GetDocumentMessage()
		return &Media{Kind: media.Document, Mime: dm.GetMimetype(), Name: dm.GetFileName(), Size: int64(dm.GetFileLength()), Key: dm.GetMediaKey(), DirectPath: dm.GetDirectPath(), FileSHA256: dm.GetFileSHA256(), FileEncSHA256: dm.GetFileEncSHA256(), Thumbnail: dm.GetJPEGThumbnail(),
			ThumbDirectPath: dm.GetThumbnailDirectPath(), ThumbSHA256: dm.GetThumbnailSHA256(), ThumbEncSHA256: dm.GetThumbnailEncSHA256()}
	}
	return nil
}

func (c *Client) Download(ref Media, dst *os.File) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	return c.cli.DownloadMediaWithPathToFile(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256, ref.Key, whatsmeow.MediaType(ref.Kind.Info()), "", false, dst)
}

// DownloadThumbnail uses the media key and key type of the file itself; whatsmeow's own DownloadThumbnail knows only link previews.
func (c *Client) DownloadThumbnail(ref Media) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	return c.cli.DownloadMediaWithPath(ctx, ref.ThumbDirectPath, ref.ThumbEncSHA256, ref.ThumbSHA256, ref.Key, whatsmeow.MediaType(ref.Kind.Info()), "", false)
}

// phone is the phone JID of a user JID that whatsmeow gives out. Any alt that holds the phone JID wins over the LID store.
func (c *Client) phone(jid types.JID, alts ...types.JID) types.JID {
	return resolvePhone(jid, c.lookupPhone, alts...)
}

func (c *Client) lookupPhone(lid types.JID) (types.JID, error) {
	if own := c.cli.Store.ID; own != nil && lid.User == c.cli.Store.GetLID().User {
		return own.ToNonAD(), nil
	}
	if c.cli.Store.LIDs == nil {
		return types.EmptyJID, errors.New("no LID store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return c.cli.Store.LIDs.GetPNForLID(ctx, lid)
}

// phoneOfConversation is the phone JID that a History Sync conversation carries, or the empty JID.
func phoneOfConversation(s string) types.JID {
	jid, err := types.ParseJID(s)
	if err != nil {
		return types.EmptyJID
	}
	if jid.Server == types.LegacyUserServer {
		jid.Server = types.DefaultUserServer
	}
	if jid.Server != types.DefaultUserServer {
		return types.EmptyJID
	}
	return jid.ToNonAD()
}

// rememberPhone stores a LID and its phone JID, so live events for that LID find the same Chat.
// A failed write only costs the mapping: the lookup then falls back to the LID.
func (c *Client) rememberPhone(lid, pn types.JID) {
	if c.cli.Store.LIDs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	_ = c.cli.Store.LIDs.PutLIDMapping(ctx, lid.ToNonAD(), pn)
}

// phoneString is phone for a JID string. A string that does not parse stays as it is.
func (c *Client) phoneString(s string) string {
	jid, err := types.ParseJID(s)
	if err != nil || jid.IsEmpty() {
		return s
	}
	return c.phone(jid).String()
}

func (c *Client) phoneStrings(in []string) []string {
	var out []string
	for _, s := range in {
		out = append(out, c.phoneString(s))
	}
	return out
}

// chatOf is the phone JID of the Chat of an event. In a Direct Chat the alt of the other user is the sender alt, or the recipient alt for the user's own Messages.
func (c *Client) chatOf(src types.MessageSource) types.JID {
	alt := src.SenderAlt
	if src.IsFromMe {
		alt = src.RecipientAlt
	}
	return c.phone(src.Chat, alt)
}

func (c *Client) senderOf(src types.MessageSource) types.JID {
	return c.phone(src.Sender, src.SenderAlt)
}

// resolvePhone maps a LID to the phone JID: first an alt that is a phone JID, then lookup. Other JIDs lose only their device part.
// shortcut: an unknown LID stays as it is, so its Messages open a separate Chat.
func resolvePhone(jid types.JID, lookup func(types.JID) (types.JID, error), alts ...types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	for _, alt := range alts {
		if alt.Server == types.DefaultUserServer {
			return alt.ToNonAD()
		}
	}
	if pn, err := lookup(jid); err == nil && !pn.IsEmpty() {
		return pn.ToNonAD()
	}
	return jid
}
