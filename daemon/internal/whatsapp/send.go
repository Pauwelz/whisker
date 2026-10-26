package whatsapp

import (
	"context"
	"errors"
	"os"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

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

// RequestHistory sends the On-Demand History Sync request to the phone, as a peer Message to the user's own ID.
func (c *Client) RequestHistory(chat string, anchor HistoryAnchor, count int) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, IsFromMe: anchor.FromMe}, ID: anchor.ID, Timestamp: time.Unix(anchor.Time, 0)}
	ctx, cancel := context.WithTimeout(context.Background(), editTimeout)
	defer cancel()
	_, err = c.cli.SendPeerMessage(ctx, c.cli.BuildHistorySyncRequest(&info, count))
	return offline(err)
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

func (c *Client) MarkReadSelf(chat, sender string, ids []string) error {
	return c.receipt(chat, sender, ids, types.ReceiptTypeReadSelf)
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
