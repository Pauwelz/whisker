package whatsapp

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// pairTimeout bounds the PairPhone network call. sendTimeout bounds one Send.
const (
	pairTimeout = 30 * time.Second
	sendTimeout = 90 * time.Second
)

// Client is the whatsmeow adapter: the only place whatsmeow types appear.
// Tests never use it: it would reach the WhatsApp servers.
type Client struct {
	cli    *whatsmeow.Client
	events chan Event
	mu     sync.Mutex // one Connect at a time
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

func (c *Client) Send(chat, id, text string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_, err = c.cli.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)}, whatsmeow.SendRequestExtra{ID: id})
	if errors.Is(err, whatsmeow.ErrNotConnected) {
		return ErrOffline
	}
	return err
}

func (c *Client) MarkRead(chat, sender string, ids []string) error {
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
	err = c.cli.MarkRead(ctx, ids, time.Now(), chatJID, senderJID)
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

func (c *Client) handle(evt any) {
	switch evt := evt.(type) {
	case *events.Connected:
		c.events <- Connected{}
	case *events.Disconnected:
		c.events <- Disconnected{}
	case *events.Message:
		// Edits, revokes and Reactions have no text yet, and nor has Media: they are skipped until their tickets.
		text := messageText(evt.Message)
		if text == "" || evt.IsEdit || evt.Info.Chat.Server == types.BroadcastServer {
			return
		}
		c.events <- NewMessage{Chat: evt.Info.Chat.String(), Kind: c.kindOf(evt.Info.Chat), Message: Message{
			ID: evt.Info.ID, Sender: evt.Info.Sender.String(), FromMe: evt.Info.IsFromMe,
			Time: evt.Info.Timestamp.Unix(), Text: text, PushName: evt.Info.PushName,
		}}
	case *events.Receipt:
		// shortcut: a Group Chat needs every recipient to reach a state, and that is not tracked yet, so its receipts are skipped.
		if state := receiptState(evt.Type); state != "" && !evt.IsGroup {
			ids := make([]string, len(evt.MessageIDs))
			for i, id := range evt.MessageIDs {
				ids[i] = id
			}
			c.events <- Receipt{Chat: evt.Chat.String(), IDs: ids, State: state}
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
		c.events <- c.historySync(evt.Data)
	case *events.PushName:
		c.events <- PushName{JID: evt.JID.String(), Name: evt.NewPushName}
	case *events.Contact:
		c.events <- ContactName{JID: evt.JID.String(), Name: evt.Action.GetFullName()}
	case *events.Pin:
		c.events <- Pin{JID: evt.JID.String(), Pinned: evt.Action.GetPinned()}
	case *events.Mute:
		until := int64(0)
		if evt.Action.GetMuted() {
			until = muteUntil(evt.Action.GetMuteEndTimestamp())
		}
		c.events <- Mute{JID: evt.JID.String(), Until: until}
	case *events.MarkChatAsRead:
		c.events <- ReadMarker{JID: evt.JID.String(), Unread: !evt.Action.GetRead()}
	}
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

// historySync maps one History Sync batch. Chats that do not parse are dropped.
func (c *Client) historySync(data *waHistorySync.HistorySync) HistorySync {
	batch := HistorySync{}
	for _, p := range data.GetPushnames() {
		batch.PushNames = append(batch.PushNames, PushName{JID: p.GetID(), Name: p.GetPushname()})
	}
	for _, conv := range data.GetConversations() {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		chat := HistoryChat{
			JID:        jid.String(),
			Kind:       c.kindOf(jid),
			LastActive: int64(max(conv.GetConversationTimestamp(), conv.GetLastMsgTimestamp())),
			Pinned:     conv.GetPinned() != 0,
			Unread:     conv.GetUnreadCount() > 0 || conv.GetMarkedAsUnread(),
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
			chat.Messages = append(chat.Messages, Message{
				ID:       parsed.Info.ID,
				Sender:   parsed.Info.Sender.String(),
				FromMe:   parsed.Info.IsFromMe,
				Time:     parsed.Info.Timestamp.Unix(),
				Text:     messageText(parsed.Message),
				PushName: parsed.Info.PushName,
			})
		}
		batch.Chats = append(batch.Chats, chat)
	}
	return batch
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

// messageText is the text of a Message. Media and system Messages have none yet.
func messageText(m *waE2E.Message) string {
	if text := m.GetConversation(); text != "" {
		return text
	}
	return m.GetExtendedTextMessage().GetText()
}
