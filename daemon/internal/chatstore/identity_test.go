package chatstore_test

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const jef = "3201@s.whatsapp.net"

func identity(id string, time int64) whatsapp.NewMessage {
	return whatsapp.NewMessage{Chat: jef, Kind: whatsapp.KindDirect,
		Message: whatsapp.Message{ID: id, Time: time, System: &whatsapp.System{Kind: whatsapp.SystemIdentity}}}
}

func lines(t *testing.T, s *chatstore.Store, chat string) (out []string) {
	t.Helper()
	page, err := s.Messages(chat, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range page.Messages {
		out = append(out, m.Text)
	}
	return out
}

func putSystem(t *testing.T, s *chatstore.Store, ev whatsapp.NewMessage) bool {
	t.Helper()
	stored, err := s.PutSystem(ev)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestSecurityCodeChangeLandsInTheDirectChatOnceAndStaysQuiet(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jef, Kind: whatsapp.KindDirect, LastActive: 5,
		Messages: []whatsapp.Message{{ID: "m1", Sender: jef, Time: 5, Text: "hi"}}}}})
	if !putSystem(t, s, identity("s1", 10)) {
		t.Fatal("the first change was not stored")
	}
	if putSystem(t, s, identity("s2", 11)) {
		t.Error("a repeat right after it was stored")
	}
	if got := lines(t, s, jef); len(got) != 2 || got[1] != "Security code changed" {
		t.Errorf("Messages = %q, want the Message and one notice", got)
	}
	page, err := s.List(0, 10, "", false)
	if err != nil || len(page.Chats) != 1 || page.Chats[0].Unread || page.Chats[0].UnreadCount != 0 || page.Chats[0].LastActive != 5 {
		t.Errorf("Chat list = %+v, %v; want a quiet Chat that keeps its place", page, err)
	}
	// A Message in between lets the next change show again.
	if _, _, err := s.PutLive(whatsapp.NewMessage{Chat: jef, Message: whatsapp.Message{ID: "m2", Sender: jef, Time: 12, Text: "yo"}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	if !putSystem(t, s, identity("s3", 13)) {
		t.Error("a change after a new Message was collapsed")
	}
}

func TestSecurityCodeChangeAddsNoChat(t *testing.T) {
	s, _ := newStore(t)
	if putSystem(t, s, identity("s1", 10)) {
		t.Error("a change stored a Chat that did not exist")
	}
	if page, _ := s.List(0, 10, "", false); len(page.Chats) != 0 {
		t.Errorf("Chats = %+v, want none", page.Chats)
	}
}

func TestOwnDevicesFirstListFillsTheSetSilently(t *testing.T) {
	s, _ := newStore(t)
	for _, tc := range []struct {
		ids  []uint16
		want bool
	}{
		{[]uint16{0, 3}, false}, // the first list
		{[]uint16{0, 3}, false}, // the same list
		{[]uint16{0, 3, 7}, true},
		{[]uint16{0, 3, 7}, false}, // the notice shows once
		{[]uint16{0, 7}, false},    // a removed device shows nothing
		{[]uint16{0, 3, 7}, true},  // a removed device that comes back is new
	} {
		if got, err := s.NoteOwnDevices(tc.ids); err != nil || got != tc.want {
			t.Errorf("NoteOwnDevices(%v) = %v, %v; want %v", tc.ids, got, err, tc.want)
		}
	}
}
