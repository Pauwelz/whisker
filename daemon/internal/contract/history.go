package contract

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	// historyCount is how many older Messages one On-Demand History Sync asks for.
	historyCount = 50
	// HistoryTimeout is how long an open request waits for the phone.
	HistoryTimeout = 30 * time.Second
)

// historyEnd ends the open request of a Chat as unanswered: its timer fired, or sending it failed.
// gen tells a stale end from the end of the request that is open now.
type historyEnd struct {
	chat string
	gen  int
}

// history follows the open On-Demand History Sync requests, one per Chat. The answer names no request, only its Chats.
// It runs on the Daemon loop only. Only the send to the phone runs on another goroutine.
type history struct {
	port   whatsapp.Port
	send   func(map[string]any) bool
	logOut io.Writer
	gen    int
	open   map[string]int // Chat to the generation of its open request
	ended  chan historyEnd
}

func newHistory(port whatsapp.Port, send func(map[string]any) bool, logOut io.Writer) *history {
	return &history{port: port, send: send, logOut: logOut, open: map[string]int{}, ended: make(chan historyEnd, 64)}
}

// received tells the Plugin how a request ended. A timeout is a count of 0 with timedOut set.
func (h *history) received(chat string, count int, timedOut bool) bool {
	params := map[string]any{"chat": chat, "count": count}
	if timedOut {
		params["timedOut"] = true
	}
	return h.send(notification("historyReceived", params))
}

// start opens the request of a Chat and sends it to the phone. A send that fails ends the request as unanswered.
func (h *history) start(chat string, anchor whatsapp.HistoryAnchor) {
	h.gen++
	end := historyEnd{chat, h.gen}
	h.open[chat] = end.gen
	timer := Clock.After(HistoryTimeout)
	go func() {
		<-timer
		h.ended <- end
	}()
	go func() {
		if err := h.port.RequestHistory(chat, anchor, historyCount); err != nil {
			fmt.Fprintln(h.logOut, "history request failed") // the error text could hold names
			h.ended <- end
		}
	}()
}

// answered closes the open request of each Chat of an On-Demand batch. added is the number of new Messages per Chat.
func (h *history) answered(batch whatsapp.HistorySync, added map[string]int) bool {
	for _, c := range batch.Chats {
		if _, ok := h.open[c.JID]; !ok {
			continue
		}
		delete(h.open, c.JID)
		if !h.received(c.JID, added[c.JID], false) {
			return false
		}
	}
	return true
}

// timedOut closes the request that e names, unless an answer or a newer request came first.
func (h *history) timedOut(e historyEnd) bool {
	if h.open[e.chat] != e.gen {
		return true
	}
	delete(h.open, e.chat)
	return h.received(e.chat, 0, true)
}

// disconnected closes every open request: the answer cannot come over a dropped connection.
func (h *history) disconnected() bool {
	chats := make([]string, 0, len(h.open))
	for chat := range h.open {
		chats = append(chats, chat)
	}
	slices.Sort(chats)
	clear(h.open)
	for _, chat := range chats {
		if !h.received(chat, 0, true) {
			return false
		}
	}
	return true
}

// chatRequestHistory asks the phone for the Messages before the oldest stored Message of a Chat.
// A Chat with no stored Message has nothing to anchor on: the phone is not asked and the Plugin hears of it at once.
func (s *session) chatRequestHistory(req *request) outcome {
	chat := req.Params.Chat
	if !s.box.online {
		return outcome{reply: errorReply(req.ID, errNoWhatsApp, msgNoWhatsApp)}
	}
	if _, ok := s.hist.open[chat]; ok {
		return outcome{reply: emptyReply(req)}
	}
	anchor, err := s.chats.OldestMessage(chat)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return outcome{emptyReply(req), func() int { return exitIf(s.hist.received(chat, 0, false)) }}
	case err != nil:
		return outcome{reply: s.storeFailed(req)}
	}
	s.hist.start(chat, anchor)
	return outcome{reply: emptyReply(req)}
}
