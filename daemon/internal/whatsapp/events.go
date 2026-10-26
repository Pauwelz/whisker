package whatsapp

import (
	"cmp"
	"context"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

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
		go c.emitOwnDevices()
	case *events.IdentityChange: // implicit or announced: the user sees the change either way
		chat := c.phone(evt.JID)
		c.emitSystem(chat, c.systemMessage(chat, types.EmptyJID, types.EmptyJID, SystemIdentity, nil, "", evt.Timestamp))
	case *events.Disconnected:
		c.events <- Disconnected{}
	case *events.StreamReplaced: // another client took the session: connecting again would start a fight over it
		c.events <- Disconnected{Reason: DisconnectReplaced}
	case *events.TemporaryBan:
		c.events <- Disconnected{Reason: DisconnectTemporaryBan}
	case *events.ConnectFailure:
		c.events <- Disconnected{Reason: DisconnectConnectFailure}
	case *events.Message:
		if ReservedID(evt.Info.ID) {
			return
		}
		c.emitLiveAlias(evt.Info.MessageSource)
		if c.handleProtocol(evt) || c.handleSecretEdit(evt) {
			return
		}
		chat := c.chatOf(evt.Info.MessageSource)
		if r := evt.Message.GetReactionMessage(); r != nil {
			if ReservedID(r.GetKey().GetID()) || (r.GetText() != "" && !slices.Contains(ReactionEmoji, r.GetText())) { // empty text removes
				return
			}
			c.events <- Reaction{Chat: chat.String(), ID: r.GetKey().GetID(), Sender: c.senderOf(evt.Info.MessageSource).String(), FromMe: evt.Info.IsFromMe, Emoji: r.GetText()}
			return
		}
		body, ok := contentOf(evt.Message, evt.IsViewOnce)
		if !ok || evt.IsEdit || chat.Server == types.BroadcastServer {
			return
		}
		c.events <- NewMessage{Chat: chat.String(), Kind: c.kindOf(chat), Ephemeral: evt.IsEphemeral, Message: c.messageOf(evt.Info, evt.Message, body)}
	case *events.Receipt:
		if c.isOwn(evt.Sender) && (evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf) {
			c.events <- ReadElsewhere{Chat: c.chatOf(evt.MessageSource).String(), IDs: slices.Clone(evt.MessageIDs)}
			return
		}
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
		if evt.JID.Server == types.GroupServer {
			c.emitSystem(evt.JID, c.systemMessage(evt.JID, evt.Author, types.EmptyJID, SystemIcon, nil, deletedIf(evt.Remove), evt.Timestamp))
		}
	case *events.GroupInfo:
		if evt.Ephemeral != nil {
			c.events <- ChatTimer{Chat: evt.JID.String(), Seconds: int64(groupTimer(evt.Ephemeral))}
		}
		for _, m := range c.groupInfoMessages(evt) {
			c.emitSystem(evt.JID, m)
		}
	case *events.JoinedGroup:
		c.emitSystem(evt.JID, c.joinedGroupMessage(evt))
	case *events.Pin:
		c.emitFlag(Pin{JID: c.phone(evt.JID).String(), Pinned: evt.Action.GetPinned()}, evt.FromFullSync)
	case *events.Archive:
		c.emitFlag(Archive{JID: c.phone(evt.JID).String(), Archived: evt.Action.GetArchived()}, evt.FromFullSync)
	case *events.DeleteForMe: // these three apply at once, also in a full sync: a Message that is gone must not wait for the sync to end
		c.events <- DeletedForMe{Chat: c.phone(evt.ChatJID).String(), ID: evt.MessageID}
	case *events.ClearChat:
		c.events <- ChatCleared{Chat: c.phone(evt.JID).String(), Until: evt.Action.GetMessageRange().GetLastMessageTimestamp()}
	case *events.DeleteChat:
		c.events <- ChatDeleted{Chat: c.phone(evt.JID).String(), Until: evt.Action.GetMessageRange().GetLastMessageTimestamp()}
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

// emitOwnDevices reads the device list of the user's own account. whatsmeow sends no event when it changes.
// It runs off the event loop: the call can block.
// shortcut: the sends below can block for ever after Disconnect when nobody reads events; every other send of this Client does the same, upgrade with a done channel for all of them.
// shortcut: whatsmeow does not check the signed list, so the server decides what this list holds (docs/architecture.md, Threat boundary).
func (c *Client) emitOwnDevices() {
	id := c.cli.Store.GetJID()
	if id.IsEmpty() {
		return
	}
	own := id.ToNonAD()
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	devices, err := c.cli.GetUserDevices(ctx, []types.JID{own})
	ids, ok := deviceIDs(devices, err)
	if !ok {
		c.events <- OwnDevices{Failed: true}
		return
	}
	c.events <- OwnDevices{Chat: own.String(), IDs: ids, Notice: c.systemMessage(own, types.EmptyJID, types.EmptyJID, SystemDevice, nil, "", time.Now())}
}

// deviceIDs is the device ids of a list. An empty list counts as a failure: the user always has this device,
// and an empty list would clear the stored set.
func deviceIDs(devices []types.JID, err error) ([]uint16, bool) {
	if err != nil || len(devices) == 0 {
		return nil, false
	}
	ids := make([]uint16, len(devices))
	for i, d := range devices {
		ids[i] = d.Device
	}
	return ids, true
}

// emitSystem sends a live System Message of a Chat.
func (c *Client) emitSystem(chat types.JID, m Message) {
	c.events <- NewMessage{Chat: chat.String(), Kind: c.kindOf(chat), Message: m}
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
	if ReservedID(id) && (pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT || pm.GetType() == waE2E.ProtocolMessage_REVOKE) {
		return true
	}
	switch {
	case pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT:
		if edit, ok := c.editOf(evt, id, pm.GetEditedMessage()); ok {
			c.events <- edit
		}
	case pm.GetType() == waE2E.ProtocolMessage_REVOKE:
		c.events <- MessageRevoked{Chat: chat, ID: id, Sender: c.senderOf(evt.Info.MessageSource).String(), FromMe: evt.Info.IsFromMe}
	case pm.GetType() == waE2E.ProtocolMessage_EPHEMERAL_SETTING:
		src := evt.Info.MessageSource
		if src.IsGroup { // any member can send this; only GroupInfo carries the server's say on a group timer
			return true
		}
		c.events <- ChatTimer{Chat: chat, Seconds: int64(pm.GetEphemeralExpiration())}
		jid := c.chatOf(src)
		c.emitSystem(jid, c.systemMessage(jid, src.Sender, src.SenderAlt, SystemTimer, nil, strconv.FormatUint(uint64(pm.GetEphemeralExpiration()), 10), evt.Info.Timestamp))
	default:
		return false
	}
	return true
}

// handleSecretEdit turns an encrypted edit into MessageEdited. WhatsApp sends the edits of other people this way.
// It reports whether the Message was an encrypted edit. An edit that does not decrypt is dropped.
func (c *Client) handleSecretEdit(evt *events.Message) bool {
	enc := evt.Message.GetSecretEncryptedMessage()
	if enc.GetSecretEncType() != waE2E.SecretEncryptedMessage_MESSAGE_EDIT {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	m, err := c.cli.DecryptSecretEncryptedMessage(ctx, evt)
	if err != nil {
		return true
	}
	if inner := m.GetProtocolMessage().GetEditedMessage(); inner != nil { // the payload can hold the edit as a ProtocolMessage
		m = inner
	}
	if edit, ok := c.editOf(evt, enc.GetTargetMessageKey().GetID(), m); ok {
		c.events <- edit
	}
	return true
}

// editOf is the MessageEdited that an event makes for the Message id, with m as its new content. It reports false for an edit without text or with a reserved target.
func (c *Client) editOf(evt *events.Message, id string, m *waE2E.Message) (MessageEdited, bool) {
	text := messageText(m)
	if text == "" || ReservedID(id) {
		return MessageEdited{}, false
	}
	mentions := mentionsOf(m)
	return MessageEdited{
		Chat: c.chatOf(evt.Info.MessageSource).String(), ID: id, Sender: c.senderOf(evt.Info.MessageSource).String(), FromMe: evt.Info.IsFromMe, Text: text,
		Time:     evt.Info.Timestamp.Unix(), // the edit's own time, so an edit that arrives late still applies
		Mentions: c.phoneStrings(MentionsIn(text, mentions)), MentionsMe: mentionsUser(mentions, c.cli.Store.GetJID().User, c.cli.Store.GetLID().User),
	}, true
}

// timerOf is the Disappearing timer in seconds that a Message carries in its ContextInfo, or 0.
func timerOf(m *waE2E.Message) int64 {
	return int64(contextOf(m).GetExpiration())
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
	batch := HistorySync{OnDemand: data.GetSyncType() == waHistorySync.HistorySync_ON_DEMAND}
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
			chat.Subject = CleanName(conv.GetName(), MaxGroupSubject)
		}
		if conv.GetMuteEndTime() != 0 {
			chat.MutedUntil = muteUntil(int64(conv.GetMuteEndTime()))
		}
		for _, hm := range conv.GetMessages() {
			// A stub can lack a sender, so it is read before ParseWebMessage. A stub without content that whisker does not show is dropped.
			if wm := hm.GetMessage(); wm.GetMessageStubType() != waWeb.WebMessageInfo_UNKNOWN {
				if m, ok := c.stubMessage(jid, wm); ok {
					chat.Messages = append(chat.Messages, m)
					continue
				}
				if wm.GetMessage() == nil {
					continue
				}
			}
			parsed, err := c.cli.ParseWebMessage(jid, hm.GetMessage())
			if err != nil || ReservedID(parsed.Info.ID) {
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
		if !slices.Contains(ReactionEmoji, r.GetText()) { // also drops the empty text of a removed Reaction
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
