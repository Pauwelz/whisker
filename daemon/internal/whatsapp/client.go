package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

// pairTimeout bounds the PairPhone network call. sendTimeout bounds one Send.
// editTimeout bounds one Edit or Revoke, which block the Daemon loop.
const (
	pairTimeout = 30 * time.Second
	sendTimeout = 90 * time.Second
	// shortcut: one fixed limit for every download, upgrade to a size based limit if a large video times out.
	downloadTimeout = 10 * time.Minute
	editTimeout     = 15 * time.Second
	uploadTimeout   = 10 * time.Minute
)

// Client is the whatsmeow adapter: the only place whatsmeow types appear.
// Tests never use it: it would reach the WhatsApp servers.
type Client struct {
	cli    *whatsmeow.Client
	events chan Event
	mu     sync.Mutex // one Connect at a time
}

// SetDeviceIdentity names the linked device "whisker" on the phone. Call it once, before the first connect and pair.
// A version that is not "major.minor.patch" at its start counts as 0.0.0.
func SetDeviceIdentity(version string) {
	var v [3]uint32
	fmt.Sscanf(version, "%d.%d.%d", &v[0], &v[1], &v[2])
	store.SetOSInfo("whisker", v)
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
}

// NewClient loads the device from the device store (a new unpaired device if there is none).
func NewClient(container *sqlstore.Container) (*Client, error) {
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop), events: make(chan Event, 64)}
	c.cli.AddEventHandler(c.handle)
	return c, nil
}

func (c *Client) Linked() bool         { return c.cli.Store.ID != nil }
func (c *Client) Events() <-chan Event { return c.events }
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
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	code, err := c.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if errors.Is(err, whatsmeow.ErrPhoneNumberTooShort) || errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational) {
		return "", ErrInvalidPhone
	}
	return code, err
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
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	err = c.cli.SendAppState(ctx, appstate.BuildMarkChatAsRead(jid, true, time.Now(), nil))
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return ErrOffline
	}
	return err
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
		// shortcut: a Group Chat needs every recipient to reach a state, and that is not tracked yet, so its receipts are skipped.
		if state := receiptState(evt.Type); state != "" && !evt.IsGroup {
			ids := make([]string, len(evt.MessageIDs))
			for i, id := range evt.MessageIDs {
				ids[i] = id
			}
			c.events <- Receipt{Chat: c.chatOf(evt.MessageSource).String(), IDs: ids, State: state}
		}
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
	case *events.Contact:
		c.events <- ContactName{JID: c.phone(evt.JID).String(), Name: evt.Action.GetFullName()}
	case *events.Pin:
		c.events <- Pin{JID: c.phone(evt.JID).String(), Pinned: evt.Action.GetPinned()}
	case *events.Mute:
		until := int64(0)
		if evt.Action.GetMuted() {
			until = muteUntil(evt.Action.GetMuteEndTimestamp())
		}
		c.events <- Mute{JID: c.phone(evt.JID).String(), Until: until}
	case *events.MarkChatAsRead:
		c.events <- ReadMarker{JID: c.phone(evt.JID).String(), Unread: !evt.Action.GetRead()}
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

// muteUntil maps a WhatsApp mute end (Unix seconds, -1 or 0 for ever) to the port value.
func muteUntil(end int64) int64 {
	if end <= 0 {
		return -1
	}
	return end
}

// aliasOf is the alias of a LID and a phone JID, or false when they are not such a pair.
func aliasOf(lid, phone types.JID) (ChatAlias, bool) {
	if lid.Server != types.HiddenUserServer || phone.Server != types.DefaultUserServer {
		return ChatAlias{}, false
	}
	return ChatAlias{LID: lid.ToNonAD().String(), Phone: phone.ToNonAD().String()}, true
}

// historyAliases are the LID to phone pairs in the mappings of a History Sync batch.
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
	return out
}

// emitLiveAlias tells the chat store that the LID of a live event and the phone JID it resolved to are one Chat.
func (c *Client) emitLiveAlias(src types.MessageSource) {
	if a, ok := aliasOf(src.Chat, c.chatOf(src)); ok {
		c.events <- a
	}
}

// historySync maps one History Sync batch, and lists the aliases that the batch taught. Chats that do not parse are dropped.
func (c *Client) historySync(data *waHistorySync.HistorySync) (HistorySync, []ChatAlias) {
	batch := HistorySync{}
	aliases := historyAliases(data)
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
			if a, ok := aliasOf(jid, pn); ok {
				aliases = append(aliases, a)
			}
		}
		jid = c.phone(jid, pn)
		chat := HistoryChat{
			JID:        jid.String(),
			Kind:       c.kindOf(jid),
			LastActive: int64(max(conv.GetConversationTimestamp(), conv.GetLastMsgTimestamp())),
			Pinned:     conv.GetPinned() != 0,
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
				chat.Messages = append(chat.Messages, c.messageOf(parsed.Info, parsed.Message, body))
			}
		}
		batch.Chats = append(batch.Chats, chat)
	}
	return batch, aliases
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
		return &Media{Kind: media.Image, Mime: im.GetMimetype(), Size: int64(im.GetFileLength()), Key: im.GetMediaKey(), DirectPath: im.GetDirectPath(), FileSHA256: im.GetFileSHA256(), FileEncSHA256: im.GetFileEncSHA256()}
	case m.GetStickerMessage() != nil:
		sm := m.GetStickerMessage()
		return &Media{Kind: media.Sticker, Mime: sm.GetMimetype(), Size: int64(sm.GetFileLength()), Key: sm.GetMediaKey(), DirectPath: sm.GetDirectPath(), FileSHA256: sm.GetFileSHA256(), FileEncSHA256: sm.GetFileEncSHA256()}
	case m.GetAudioMessage() != nil:
		am := m.GetAudioMessage()
		kind := media.Audio
		if am.GetPTT() {
			kind = media.Voice
		}
		return &Media{Kind: kind, Mime: am.GetMimetype(), Size: int64(am.GetFileLength()), Key: am.GetMediaKey(), DirectPath: am.GetDirectPath(), FileSHA256: am.GetFileSHA256(), FileEncSHA256: am.GetFileEncSHA256()}
	case m.GetVideoMessage() != nil:
		vm := m.GetVideoMessage()
		return &Media{Kind: media.Video, Mime: vm.GetMimetype(), Size: int64(vm.GetFileLength()), Key: vm.GetMediaKey(), DirectPath: vm.GetDirectPath(), FileSHA256: vm.GetFileSHA256(), FileEncSHA256: vm.GetFileEncSHA256()}
	case m.GetDocumentMessage() != nil:
		dm := m.GetDocumentMessage()
		return &Media{Kind: media.Document, Mime: dm.GetMimetype(), Name: dm.GetFileName(), Size: int64(dm.GetFileLength()), Key: dm.GetMediaKey(), DirectPath: dm.GetDirectPath(), FileSHA256: dm.GetFileSHA256(), FileEncSHA256: dm.GetFileEncSHA256()}
	}
	return nil
}

func (c *Client) Download(ref Media, dst *os.File) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	return c.cli.DownloadMediaWithPathToFile(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256, ref.Key, whatsmeow.MediaType(ref.Kind.Info()), "", false, dst)
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
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
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
