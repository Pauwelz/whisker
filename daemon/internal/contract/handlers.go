package contract

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	methodKeyringRetry    = "keyring.retry"
	methodLinkPairCode    = "link.pairCode"
	methodLinkRetry       = "link.retry"
	methodChatsList       = "chats.list"
	methodChatMessages    = "chat.messages"
	methodChatStart       = "chat.start"
	methodContactsSearch  = "contacts.search"
	methodContactsLookup  = "contacts.lookup"
	methodChatSend        = "chat.send"
	methodChatRetry       = "chat.retry"
	methodChatSendMedia   = "chat.sendMedia"
	methodAttentionSet    = "attention.set"
	methodMediaDownload   = "media.download"
	methodMediaOpen       = "media.open"
	methodMediaSave       = "media.save"
	methodMediaRelease    = "media.release"
	methodMediaPlayed     = "media.played"
	methodChatEdit        = "chat.edit"
	methodChatRevoke      = "chat.revoke"
	methodChatReact       = "chat.react"
	methodChatTyping      = "chat.typing"
	methodChatPin         = "chat.pin"
	methodChatMute        = "chat.mute"
	methodChatMarkUnread  = "chat.markUnread"
	methodChatDeleteForMe = "chat.deleteForMe"
	methodDraftGet        = "draft.get"
	methodDraftSet        = "draft.set"
	methodSettingsGet     = "settings.get"
	methodSettingsSet     = "settings.set"
	methodLogout          = "profile.logout"
)

// request is one line from the Plugin. Params holds the union of the params of all methods.
type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Phone      string `json:"phone"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
		Query      string `json:"query"`
		Chat       string `json:"chat"`
		JID        string `json:"jid"`
		Text       string `json:"text"`
		Caption    string `json:"caption"`
		Open       bool   `json:"open"`
		Pinned     bool   `json:"pinned"`
		Unread     bool   `json:"unread"`
		Until      *int64 `json:"until"` // nil when the request has none
		Typing     bool   `json:"typing"`
		ID         string `json:"id"`
		Emoji      string `json:"emoji"`
		Quote      string `json:"quote"`
		Edit       string `json:"edit"`
		Path       string `json:"path"`
		Attachment string `json:"attachment"`
		settings.Update
	} `json:"params"`
}

// outcome is what a handler gives back: the reply, and the follow-up work that runs after the reply is sent.
// then returns an exit code, or keepGoing. It may be nil.
type outcome struct {
	reply map[string]any
	then  func() int
}

// when says in which state of the session a method exists. Any other time, the method is "not found".
type when int

const (
	whenWaitingForKeyring when = iota // the Profile is not open yet
	whenLinking                       // open, and the Profile is not linked
	whenReady                         // open
	whenLinked                        // open and linked
)

func (s *session) available(w when) bool {
	switch w {
	case whenWaitingForKeyring:
		return !s.ready
	case whenLinking:
		return s.ready && !s.port.Linked()
	case whenLinked:
		return s.ready && s.port.Linked()
	}
	return s.ready
}

type handler struct {
	when when
	run  func(s *session, req *request) outcome
}

// handlers is the one table of methods. A new method is a new row and a new function.
var handlers = map[string]handler{
	methodKeyringRetry:    {whenWaitingForKeyring, (*session).keyringRetry},
	methodLinkRetry:       {whenLinking, (*session).linkRetry},
	methodLinkPairCode:    {whenLinking, (*session).linkPairCode},
	methodChatsList:       {whenReady, (*session).chatsList},
	methodChatMessages:    {whenReady, (*session).chatMessages},
	methodChatStart:       {whenReady, (*session).chatStart},
	methodContactsSearch:  {whenReady, (*session).contactsSearch},
	methodContactsLookup:  {whenLinked, (*session).contactsLookup},
	methodDraftGet:        {whenReady, (*session).draftGet},
	methodDraftSet:        {whenReady, (*session).draftSet},
	methodChatSend:        {whenLinked, (*session).chatSend},
	methodChatSendMedia:   {whenLinked, (*session).chatSendMedia},
	methodChatRetry:       {whenLinked, (*session).chatRetry},
	methodMediaDownload:   {whenLinked, (*session).mediaDownload},
	methodMediaOpen:       {whenReady, (*session).mediaOpen},
	methodMediaSave:       {whenReady, (*session).mediaSave},
	methodMediaRelease:    {whenReady, (*session).mediaRelease},
	methodMediaPlayed:     {whenReady, (*session).mediaPlayed},
	methodAttentionSet:    {whenReady, (*session).attentionSet},
	methodChatEdit:        {whenLinked, (*session).chatEdit},
	methodChatRevoke:      {whenLinked, (*session).chatRevoke},
	methodChatReact:       {whenLinked, (*session).chatReact},
	methodChatTyping:      {whenLinked, (*session).chatTyping},
	methodChatPin:         {whenLinked, (*session).chatPin},
	methodChatMute:        {whenLinked, (*session).chatMute},
	methodChatMarkUnread:  {whenLinked, (*session).chatMarkUnread},
	methodChatDeleteForMe: {whenLinked, (*session).chatDeleteForMe},
	methodSettingsGet:     {whenReady, (*session).settingsGet},
	methodSettingsSet:     {whenReady, (*session).settingsSet},
	methodLogout:          {whenLinked, (*session).logout},
}

// onLine answers one line from the Plugin. It returns an exit code, or keepGoing.
func (s *session) onLine(line []byte) int {
	var req request
	var out outcome
	if err := json.Unmarshal(line, &req); err != nil {
		out.reply = errorReply(nil, errParse, msgParse)
	} else if len(req.ID) == 0 {
		return keepGoing // a notification gets no reply
	} else if h, ok := handlers[req.Method]; ok && s.available(h.when) {
		out = h.run(s, &req)
	} else {
		out.reply = errorReply(req.ID, errMethodNotFound, msgMethodNotFound)
	}
	if !s.send(out.reply) {
		return ExitError
	}
	if out.then == nil {
		return keepGoing
	}
	return out.then()
}

func emptyReply(req *request) map[string]any { return resultReply(req.ID, map[string]any{}) }

// announce tells the Plugin that a Message changed.
func (s *session) announce(m chatstore.Message) bool {
	return s.send(notification("messageChanged", map[string]any{"message": m}))
}

// submit starts the send of a pending Message.
func (s *session) submit(m chatstore.Message) bool {
	s.box.submit(m)
	return true
}

// followUp is the follow-up of a request that stored or changed a Message: before runs, act runs (submit or announce), then after runs.
// before and after may be nil. Nothing runs when the request left no Message (m is nil).
func (s *session) followUp(reply map[string]any, m *chatstore.Message, before func(), act func(chatstore.Message) bool, after func()) outcome {
	return outcome{reply, func() int {
		if m == nil {
			return keepGoing
		}
		if before != nil {
			before()
		}
		ok := act(*m)
		if after != nil {
			after()
		}
		return exitIf(ok)
	}}
}

// sentOutcome is the follow-up of a send: sending uses up the Draft, and a sent Message is the interaction that the Read Receipt policy waits for.
func (s *session) sentOutcome(chat string, reply map[string]any, m *chatstore.Message) outcome {
	return s.followUp(reply, m, func() { s.clearDraft(chat) }, s.submit, func() { s.read.interaction(chat) })
}

// clearDraft uses up the Draft of a Chat: sending or editing consumes it.
func (s *session) clearDraft(chat string) {
	if err := s.chats.ClearDraft(chat); err != nil {
		fmt.Fprintln(s.logOut, "draft clear failed")
	}
}

func (s *session) keyringRetry(req *request) outcome {
	return outcome{emptyReply(req), s.open}
}

func (s *session) linkRetry(req *request) outcome {
	return outcome{emptyReply(req), func() int { return exitIf(s.connect()) }}
}

func (s *session) linkPairCode(req *request) outcome {
	// shortcut: PairCode blocks the Daemon loop (up to 30 s), upgrade when another request must run meanwhile.
	code, err := s.port.PairCode(req.Params.Phone)
	switch {
	case errors.Is(err, whatsapp.ErrInvalidPhone):
		return outcome{reply: errorReply(req.ID, errInvalidPhone, msgInvalidPhone)}
	case err != nil:
		fmt.Fprintln(s.logOut, "pair code failed") // the error text could hold the phone number
		return outcome{reply: errorReply(req.ID, errPairCode, msgPairCode)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"code": code})}
}

// pageParams applies the paging rules of chats.list and chat.messages: no negative offset, a default and a maximum limit.
func pageParams(offset, limit int) (int, int) {
	if limit <= 0 {
		limit = defaultChatsLimit
	}
	return max(offset, 0), min(limit, maxChatsLimit)
}

func (s *session) chatsList(req *request) outcome {
	p := req.Params
	offset, limit := pageParams(p.Offset, p.Limit)
	page, err := s.chats.List(offset, limit, p.Query)
	if err != nil {
		fmt.Fprintln(s.logOut, "chat list failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	unread, err := s.chats.UnreadChats()
	if err != nil {
		fmt.Fprintln(s.logOut, "chat list failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chats": page.Chats, "more": page.More, "offset": offset, "unreadChats": unread})}
}

// contactsSearch lists the people the user can start a Chat with. The limit follows the paging rules, without an offset.
func (s *session) contactsSearch(req *request) outcome {
	p := req.Params
	_, limit := pageParams(0, p.Limit)
	contacts, err := s.chats.Contacts(p.Query, limit, s.port.OwnUsers())
	if err != nil {
		return outcome{reply: s.storeFailed(req)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"contacts": contacts})}
}

// contactsLookup asks WhatsApp whether a phone number is on WhatsApp. The reply names the digits it answers for,
// so the Plugin can drop a late reply for a number that the user has edited since.
func (s *session) contactsLookup(req *request) outcome {
	// shortcut: Lookup blocks the Daemon loop (up to 15 s), upgrade when another request must run meanwhile.
	digits, jid, err := s.port.Lookup(req.Params.Phone)
	switch {
	case errors.Is(err, whatsapp.ErrInvalidPhone):
		return outcome{reply: errorReply(req.ID, errInvalidPhone, msgInvalidPhone)}
	case err != nil:
		return outcome{reply: s.whatsappFailed(req.ID, err)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"phone": digits, "jid": jid})}
}

// chatStart returns the Chat of a person, and stores an empty one when there is none. It makes no call to WhatsApp.
func (s *session) chatStart(req *request) outcome {
	if !chatstore.IsPerson(req.Params.JID) {
		return outcome{reply: errorReply(req.ID, errInvalidChat, msgInvalidChat)}
	}
	chat, err := s.chats.StartChat(req.Params.JID)
	if err != nil {
		return outcome{reply: s.storeFailed(req)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": chat})}
}

func (s *session) chatMessages(req *request) outcome {
	p := req.Params
	offset, limit := pageParams(p.Offset, p.Limit)
	page, err := s.chats.Messages(p.Chat, offset, limit)
	if err != nil {
		fmt.Fprintln(s.logOut, "message list failed")
		return outcome{reply: errorReply(req.ID, errChatStore, msgChatStore)}
	}
	return outcome{reply: resultReply(req.ID, map[string]any{"chat": p.Chat, "messages": page.Messages, "more": page.More, "offset": offset})}
}

// chatSend stores the text as a pending Message. The follow-up sends it after the reply.
// A non-empty quote makes it a reply: that Message must exist in the Chat and not be a Tombstone.
func (s *session) chatSend(req *request) outcome {
	p := req.Params
	if !validText(p.Text) {
		return outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	}
	var quote *whatsapp.Quote
	if p.Quote != "" {
		q, err := s.chats.Message(p.Chat, p.Quote)
		if err != nil || q.Revoked {
			return outcome{reply: s.storeOrInvalid(req.ID, err)}
		}
		quote = whatsappQuote(q.ID, q.Sender, q.Text)
	}
	m, err := s.chats.AddPending(p.Chat, s.port.NewMessageID(), p.Text, quote)
	reply, submit := s.messageReply(req.ID, m, err)
	return s.sentOutcome(p.Chat, reply, submit)
}

// chatRetry makes a failed Message pending again. The follow-up sends it after the reply.
func (s *session) chatRetry(req *request) outcome {
	p := req.Params
	changed, err := s.chats.Requeue(p.Chat, p.ID)
	if err == nil && !changed {
		return outcome{reply: errorReply(req.ID, errMessage, msgInvalidMessage)}
	}
	if err != nil {
		reply, _ := s.messageReply(req.ID, chatstore.Message{}, err)
		return outcome{reply: reply}
	}
	m, err := s.chats.Message(p.Chat, p.ID)
	reply, submit := s.messageReply(req.ID, m, err)
	return s.followUp(reply, submit, nil, s.submit, nil)
}

func (s *session) messageReply(id json.RawMessage, m chatstore.Message, err error) (map[string]any, *chatstore.Message) {
	switch {
	case errors.Is(err, chatstore.ErrNoChat):
		return errorReply(id, errMessage, msgInvalidMessage), nil
	case err != nil:
		fmt.Fprintln(s.logOut, "message write failed")
		return errorReply(id, errChatStore, msgChatStore), nil
	}
	return resultReply(id, map[string]any{"message": m}), &m
}

func (s *session) mediaDownload(req *request) outcome {
	chat, msgID := req.Params.Chat, req.Params.ID
	reply, started := s.loader.downloadReply(req.ID, chat, msgID)
	return outcome{reply, func() int {
		if started {
			s.loader.start(chat, msgID)
		}
		return keepGoing
	}}
}

func (s *session) mediaOpen(req *request) outcome {
	return outcome{reply: s.loader.openReply(req.ID, req.Params.Chat, req.Params.ID)}
}

func (s *session) mediaSave(req *request) outcome {
	p := req.Params
	return outcome{reply: s.loader.saveReply(req.ID, p.Chat, p.ID, p.Path)}
}

func (s *session) mediaRelease(req *request) outcome {
	s.media.Release(req.Params.Chat)
	return outcome{reply: emptyReply(req)}
}

func (s *session) mediaPlayed(req *request) outcome {
	reply, played := s.loader.playedReply(req.ID, req.Params.Chat, req.Params.ID, s.read)
	return s.followUp(reply, played, nil, s.announce, nil)
}

func (s *session) attentionSet(req *request) outcome {
	listChanged := s.read.setAttention(req.Params.Open, req.Params.Chat)
	s.pres.sync()
	return outcome{emptyReply(req), func() int {
		return exitIf(!listChanged || s.send(notification("chatsChanged", nil)))
	}}
}

func (s *session) chatTyping(req *request) outcome {
	s.pres.typing(req.Params.Chat, req.Params.Typing)
	return outcome{reply: emptyReply(req)}
}

func (s *session) settingsGet(req *request) outcome {
	return s.settingsOutcome(req.ID, func() (settings.Settings, error) { return s.prefs.Get() })
}

func (s *session) settingsSet(req *request) outcome {
	return s.settingsOutcome(req.ID, func() (settings.Settings, error) {
		cur, err := s.prefs.Set(req.Params.Update)
		s.pres.sync() // "send online" may have changed
		return cur, err
	})
}

func (s *session) settingsOutcome(id json.RawMessage, apply func() (settings.Settings, error)) outcome {
	cur, err := apply()
	switch {
	case errors.Is(err, settings.ErrInvalid):
		return outcome{reply: errorReply(id, errSetting, msgInvalidSetting)}
	case err != nil:
		fmt.Fprintln(s.logOut, "settings failed") // the error text could name paths
		return outcome{reply: errorReply(id, errSetting, msgSettingsFailed)}
	}
	return outcome{reply: resultReply(id, map[string]any{"readReceipts": cur.ReadReceipts, "sendOnline": cur.SendOnline, "sendTyping": cur.SendTyping, "notifications": cur.Notifications})}
}

// logout asks WhatsApp to remove the Companion Device, then deletes the Profile whatever the answer.
func (s *session) logout(req *request) outcome {
	// The user must get out even when WhatsApp is out of reach: the Profile goes either way.
	if err := s.port.Logout(); err != nil {
		fmt.Fprintln(s.logOut, "remote logout failed") // the error text could name hosts
	}
	return outcome{emptyReply(req), func() int { return s.unlink("user") }}
}
