package contract

import (
	"encoding/json"
	"fmt"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
)

const (
	methodKeyringRetry  = "keyring.retry"
	methodLinkPairCode  = "link.pairCode"
	methodLinkRetry     = "link.retry"
	methodChatsList     = "chats.list"
	methodChatMessages  = "chat.messages"
	methodChatSend      = "chat.send"
	methodChatRetry     = "chat.retry"
	methodChatSendMedia = "chat.sendMedia"
	methodAttentionSet  = "attention.set"
	methodMediaDownload = "media.download"
	methodMediaOpen     = "media.open"
	methodMediaSave     = "media.save"
	methodMediaRelease  = "media.release"
	methodMediaPlayed   = "media.played"
	methodChatEdit      = "chat.edit"
	methodChatRevoke    = "chat.revoke"
	methodChatReact     = "chat.react"
	methodChatTyping    = "chat.typing"
	methodDraftGet      = "draft.get"
	methodDraftSet      = "draft.set"
	methodSettingsGet   = "settings.get"
	methodSettingsSet   = "settings.set"
	methodLogout        = "profile.logout"
)

// request is one line from the Plugin. Params holds the union of the params of all methods.
type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Phone   string `json:"phone"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
		Query   string `json:"query"`
		Chat    string `json:"chat"`
		Text    string `json:"text"`
		Caption string `json:"caption"`
		Open    bool   `json:"open"`
		Typing  bool   `json:"typing"`
		ID      string `json:"id"`
		Emoji   string `json:"emoji"`
		Quote   string `json:"quote"`
		Edit    string `json:"edit"`
		Path    string `json:"path"`
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
	methodKeyringRetry:  {whenWaitingForKeyring, (*session).keyringRetry},
	methodLinkRetry:     {whenLinking, (*session).linkRetry},
	methodLinkPairCode:  {whenLinking, (*session).linkPairCode},
	methodChatsList:     {whenReady, (*session).chatsList},
	methodChatMessages:  {whenReady, (*session).chatMessages},
	methodDraftGet:      {whenReady, (*session).draftGet},
	methodDraftSet:      {whenReady, (*session).draftSet},
	methodChatSend:      {whenLinked, (*session).chatSend},
	methodChatSendMedia: {whenLinked, (*session).chatSendMedia},
	methodChatRetry:     {whenLinked, (*session).chatRetry},
	methodMediaDownload: {whenLinked, (*session).mediaDownload},
	methodMediaOpen:     {whenReady, (*session).mediaOpen},
	methodMediaSave:     {whenReady, (*session).mediaSave},
	methodMediaRelease:  {whenReady, (*session).mediaRelease},
	methodMediaPlayed:   {whenReady, (*session).mediaPlayed},
	methodAttentionSet:  {whenReady, (*session).attentionSet},
	methodChatEdit:      {whenLinked, (*session).chatEdit},
	methodChatRevoke:    {whenLinked, (*session).chatRevoke},
	methodChatReact:     {whenLinked, (*session).chatReact},
	methodChatTyping:    {whenLinked, (*session).chatTyping},
	methodSettingsGet:   {whenReady, (*session).settingsGet},
	methodSettingsSet:   {whenReady, (*session).settingsSet},
	methodLogout:        {whenLinked, (*session).logout},
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

// afterChange is the follow-up of an edit, a revoke or a Reaction: before runs, then the Plugin hears of the changed Message.
func (s *session) afterChange(reply map[string]any, changed *chatstore.Message, before func()) outcome {
	return outcome{reply, func() int {
		if changed == nil {
			return keepGoing
		}
		if before != nil {
			before()
		}
		return exitIf(s.announce(*changed))
	}}
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
	return outcome{reply: pairCodeReply(s.port, req.ID, req.Params.Phone, s.logOut)}
}

func (s *session) chatsList(req *request) outcome {
	p := req.Params
	return outcome{reply: chatsListReply(s.chats, req.ID, p.Offset, p.Limit, p.Query, s.logOut)}
}

func (s *session) chatMessages(req *request) outcome {
	p := req.Params
	return outcome{reply: chatMessagesReply(s.chats, req.ID, p.Chat, p.Offset, p.Limit, s.logOut)}
}

func (s *session) draftGet(req *request) outcome {
	return outcome{reply: draftGetReply(s.chats, req.ID, req.Params.Chat, s.logOut)}
}

func (s *session) draftSet(req *request) outcome {
	p := req.Params
	return outcome{reply: draftSetReply(s.chats, req.ID, p.Chat, chatstore.Draft{Text: p.Text, Quote: p.Quote, Edit: p.Edit}, s.logOut)}
}

// submitted is the follow-up of a request that stored a pending Message: before runs, the send starts, then after runs.
// before and after may be nil. Nothing runs when the request stored no Message.
func (s *session) submitted(reply map[string]any, submit *chatstore.Message, before, after func()) outcome {
	return outcome{reply, func() int {
		if submit == nil {
			return keepGoing
		}
		if before != nil {
			before()
		}
		s.box.submit(*submit)
		if after != nil {
			after()
		}
		return keepGoing
	}}
}

// chatSend: sending uses up the Draft, and a sent Message is the interaction that the Read Receipt policy waits for.
func (s *session) chatSend(req *request) outcome {
	p := req.Params
	reply, submit := chatSendReply(s.chats, s.port, req.ID, p.Chat, p.Text, p.Quote, s.logOut)
	return s.submitted(reply, submit, func() { s.clearDraft(p.Chat) }, func() { s.read.interaction(p.Chat) })
}

func (s *session) chatSendMedia(req *request) outcome {
	p := req.Params
	reply, submit := chatSendMediaReply(s.chats, s.media, s.port.NewMessageID, req.ID, p.Chat, p.Path, p.Caption, s.logOut)
	return s.submitted(reply, submit, nil, func() { s.read.interaction(p.Chat) })
}

func (s *session) chatRetry(req *request) outcome {
	reply, submit := chatRetryReply(s.chats, req.ID, req.Params.Chat, req.Params.ID, s.logOut)
	return s.submitted(reply, submit, nil, nil)
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
	return outcome{reply: s.loader.playedReply(req.ID, req.Params.Chat, req.Params.ID, s.read)}
}

func (s *session) attentionSet(req *request) outcome {
	listChanged := s.read.setAttention(req.Params.Open, req.Params.Chat)
	s.pres.sync()
	return outcome{emptyReply(req), func() int {
		return exitIf(!listChanged || s.send(notification("chatsChanged", nil)))
	}}
}

func (s *session) chatEdit(req *request) outcome {
	p := req.Params
	reply, changed := chatEditReply(s.chats, s.port, req.ID, p.Chat, p.ID, p.Text, s.logOut)
	return s.afterChange(reply, changed, func() { s.clearDraft(p.Chat) })
}

func (s *session) chatRevoke(req *request) outcome {
	reply, changed := chatRevokeReply(s.chats, s.port, req.ID, req.Params.Chat, req.Params.ID, s.logOut)
	return s.afterChange(reply, changed, nil)
}

// chatReact: a Reaction is an interaction, like a sent Message.
func (s *session) chatReact(req *request) outcome {
	p := req.Params
	reply, changed := chatReactReply(s.chats, s.port, req.ID, p.Chat, p.ID, p.Emoji, s.logOut)
	return s.afterChange(reply, changed, func() { s.read.interaction(p.Chat) })
}

func (s *session) chatTyping(req *request) outcome {
	s.pres.typing(req.Params.Chat, req.Params.Typing)
	return outcome{reply: emptyReply(req)}
}

func (s *session) settingsGet(req *request) outcome {
	return outcome{reply: settingsReply(req.ID, s.logOut, func() (settings.Settings, error) { return s.prefs.Get() })}
}

func (s *session) settingsSet(req *request) outcome {
	return outcome{reply: settingsReply(req.ID, s.logOut, func() (settings.Settings, error) {
		cur, err := s.prefs.Set(req.Params.Update)
		s.pres.sync() // "send online" may have changed
		return cur, err
	})}
}

// logout asks WhatsApp to remove the Companion Device, then deletes the Profile whatever the answer.
func (s *session) logout(req *request) outcome {
	// The user must get out even when WhatsApp is out of reach: the Profile goes either way.
	if err := s.port.Logout(); err != nil {
		fmt.Fprintln(s.logOut, "remote logout failed") // the error text could name hosts
	}
	return outcome{emptyReply(req), func() int { return s.unlink("user") }}
}
