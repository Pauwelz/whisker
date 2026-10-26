package contract_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	mentionChat = "1@s.whatsapp.net"
	mentionJID  = "31611111111@s.whatsapp.net"
	mentionText = "@31611111111 hi"
)

// mentionRunner starts a Daemon with one Message that mentions a JID that has a Phone Contact name.
func mentionRunner(t *testing.T, fromMe bool) *runner {
	t.Helper()
	r := startedRunner(t)
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: mentionChat, LastActive: 1}}})
	r.port.Emit(whatsapp.Connected{})
	r.port.Emit(whatsapp.ContactName{JID: mentionJID, Name: "Mom"})
	r.nextFrame() // chatsChanged
	r.port.Emit(whatsapp.NewMessage{Chat: mentionChat, Message: whatsapp.Message{
		ID: "q1", Sender: mentionChat, FromMe: fromMe, Time: 1000, Text: mentionText, Mentions: []string{mentionJID},
	}})
	// The events reach the Daemon loop asynchronously: ask until the Message shows.
	for id := 90; id < 190; id++ {
		if f := r.reply(t, id, `"method":"chat.messages","params":{"chat":"`+mentionChat+`"}`); strings.Contains(f, `"q1"`) {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the Message q1 never showed")
	return r
}

// reply sends the request with this id and returns the frame that answers it, and skips other frames.
func (r *runner) reply(t *testing.T, id int, rest string) string {
	t.Helper()
	r.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,%s}`, id, rest))
	for range 10 {
		f := r.nextFrame()
		if strings.Contains(f, fmt.Sprintf(`"id":%d,`, id)) {
			return f
		}
	}
	t.Fatalf("no answer to request %d", id)
	return ""
}

func waitSent(t *testing.T, r *runner) {
	t.Helper()
	for range 10 {
		if f := r.nextFrame(); strings.Contains(f, `"state":"sent"`) {
			return
		}
	}
	t.Fatal("the reply never reached the state sent")
}

func wantQuoteText(t *testing.T, r *runner) {
	t.Helper()
	if got := r.port.QuoteTexts(); len(got) != 1 || got[0] != mentionText {
		t.Errorf("quote text at the Port = %q, want [%q]", got, mentionText)
	}
}

// The reply quotes the raw text, and the timeline still shows the name.
func TestReplyQuotesRawTextOfMentions(t *testing.T) {
	r := mentionRunner(t, false)
	r.send(`{"jsonrpc":"2.0","id":1,"method":"chat.send","params":{"chat":"` + mentionChat + `","text":"yes","quote":"q1"}}`)
	waitSent(t, r)
	wantQuoteText(t, r)
	if f := r.reply(t, 2, `"method":"chat.messages","params":{"chat":"`+mentionChat+`"}`); !strings.Contains(f, "@Mom hi") {
		t.Errorf("frame = %s, want the name @Mom in the Message", f)
	}
}

// The quoted Message is gone at the retry: the stored snapshot still holds the raw text.
func TestRetryAfterQuotedMessageDeletedSendsRawText(t *testing.T) {
	r := mentionRunner(t, false)
	r.port.SetSendError(errors.New("boom")) // a failed send, not an offline one
	r.send(`{"jsonrpc":"2.0","id":1,"method":"chat.send","params":{"chat":"` + mentionChat + `","text":"yes","quote":"q1"}}`)
	for f := ""; !strings.Contains(f, `"state":"failed"`); f = r.nextFrame() {
	}
	r.port.SetSendError(nil) // the Fake refuses delete-for-me while it fails sends
	r.reply(t, 2, `"method":"chat.deleteForMe","params":{"chat":"`+mentionChat+`","id":"q1"}`)
	r.port.SetSendError(nil)
	r.reply(t, 3, `"method":"chat.retry","params":{"chat":"`+mentionChat+`","id":"fake-1"}`)
	waitSent(t, r)
	wantQuoteText(t, r)
}

// An edit of a Message with mentions would send the names as text and drop the mentions.
func TestEditRefusesMessageWithMentions(t *testing.T) {
	r := mentionRunner(t, true)
	r.send(`{"jsonrpc":"2.0","id":1,"method":"chat.edit","params":{"chat":"` + mentionChat + `","id":"q1","text":"@Mom bye"}}`)
	for range 3 {
		if f := r.nextFrame(); strings.Contains(f, `"id":1`) {
			if !strings.Contains(f, "-32006") {
				t.Errorf("frame = %s, want -32006", f)
			}
			if got := r.port.Edits(); len(got) != 0 {
				t.Errorf("edits at the Port = %v, want none", got)
			}
			return
		}
	}
	t.Fatal("no answer to chat.edit")
}
