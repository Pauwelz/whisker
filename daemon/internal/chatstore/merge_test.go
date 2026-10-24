package chatstore_test

import (
	"database/sql"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	lidJID   = "99@lid"
	phoneJID = "31600@s.whatsapp.net"
)

func rows(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func msg(id string, at int64) whatsapp.Message {
	return whatsapp.Message{ID: id, Sender: lidJID, Time: at, Text: id}
}

func TestMergeChatMovesEverythingAndMergesFlags(t *testing.T) {
	s, db := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: lidJID, LastActive: 50, Pinned: true, Unread: true, MutedUntil: -1, Timer: 600, Messages: []whatsapp.Message{msg("a", 10), msg("dup", 20)}},
		{JID: phoneJID, LastActive: 30, Messages: []whatsapp.Message{msg("dup", 20), msg("b", 30)}},
	}})
	apply(t, s, whatsapp.ContactName{JID: lidJID, Name: "Ann"})
	for _, q := range []string{
		`INSERT INTO reactions VALUES ('` + lidJID + `', 'a', '', '+1')`,
		`UPDATE messages SET read_pending = 1 WHERE chat_jid = '` + lidJID + `' AND id = 'a'`,
		`INSERT INTO media (chat_jid, id, kind, media_key) VALUES ('` + lidJID + `', 'a', 'image', x'00')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetDraft(lidJID, chatstore.Draft{Text: "lid draft"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraft(phoneJID, chatstore.Draft{Text: "phone draft"}); err != nil {
		t.Fatal(err)
	}

	changed, err := s.MergeChat(lidJID, phoneJID)
	if err != nil || !changed {
		t.Fatalf("MergeChat = %v, %v; want true", changed, err)
	}
	page := list(t, s, 0, 10)
	if len(page.Chats) != 1 {
		t.Fatalf("chats = %+v; want only the phone Chat", page.Chats)
	}
	c := page.Chats[0]
	if c.JID != phoneJID || c.Name != "Ann" || c.LastActive != 50 || !c.Pinned || !c.Unread || !c.Muted || c.Timer != 600 {
		t.Errorf("merged Chat = %+v", c)
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM messages WHERE chat_jid = ?`, phoneJID); n != 3 {
		t.Errorf("phone Chat has %d Messages; want 3 (a, dup, b)", n)
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM messages WHERE sender_jid = ?`, lidJID); n != 0 {
		t.Errorf("%d Messages still name the LID as author", n)
	}
	if n := rows(t, db, `SELECT read_pending FROM messages WHERE chat_jid = ? AND id = 'a'`, phoneJID); n != 1 {
		t.Error("read_pending did not move with the Message")
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM reactions WHERE chat_jid = ?`, phoneJID); n != 1 {
		t.Error("the Reaction did not move")
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM media WHERE chat_jid = ?`, phoneJID); n != 1 {
		t.Error("the Media row did not move")
	}
	if d, _ := s.Draft(phoneJID); d.Text != "phone draft" {
		t.Errorf("Draft = %+v; the phone Draft must win", d)
	}
	for _, table := range []string{"chats", "messages", "reactions", "media", "drafts"} {
		col := "chat_jid"
		if table == "chats" {
			col = "jid"
		}
		if n := rows(t, db, `SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, lidJID); n != 0 {
			t.Errorf("%s keeps %d LID rows", table, n)
		}
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM names WHERE jid = ?`, lidJID); n != 0 {
		t.Error("the LID name row stays")
	}
}

func TestMergeChatRekeysWhenNoPhoneChatExists(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: lidJID, LastActive: 5, Messages: []whatsapp.Message{msg("a", 5)}}}})
	if changed, err := s.MergeChat(lidJID, phoneJID); err != nil || !changed {
		t.Fatalf("MergeChat = %v, %v; want true", changed, err)
	}
	page := list(t, s, 0, 10)
	if len(page.Chats) != 1 || page.Chats[0].JID != phoneJID {
		t.Errorf("chats = %+v; want the Chat under the phone JID", page.Chats)
	}
	msgs, err := s.Messages(phoneJID, 0, 10)
	if err != nil || len(msgs.Messages) != 1 {
		t.Errorf("Messages = %+v, %v", msgs, err)
	}
}

func TestMergeChatOfAnUnknownLIDChangesNothing(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: phoneJID, LastActive: 5}}})
	for range 2 { // the alias can repeat
		if changed, err := s.MergeChat(lidJID, phoneJID); err != nil || changed {
			t.Fatalf("MergeChat = %v, %v; want false", changed, err)
		}
	}
	if page := list(t, s, 0, 10); len(page.Chats) != 1 {
		t.Errorf("chats = %+v", page.Chats)
	}
}
