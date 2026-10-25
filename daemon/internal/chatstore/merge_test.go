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
	if d, _ := s.Draft(phoneJID); d.Text != "phone draft\nlid draft" {
		t.Errorf("Draft = %+v; want both texts joined", d)
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

func TestMergeChatDraftKeepsPhoneTargetsAndTheOnlyDraft(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: lidJID, LastActive: 5, Messages: []whatsapp.Message{msg("a", 5)}},
		{JID: phoneJID, LastActive: 5, Messages: []whatsapp.Message{msg("b", 5)}},
	}})
	if err := s.SetDraft(lidJID, chatstore.Draft{Text: "lid", Quote: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraft(phoneJID, chatstore.Draft{Text: "phone", Quote: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeChat(lidJID, phoneJID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Draft(phoneJID); d != (chatstore.Draft{Text: "phone\nlid", Quote: "b"}) {
		t.Errorf("Draft = %+v; want the phone Quote and both texts", d)
	}

	// A lone LID Draft moves over whole.
	if err := s.SetDraft("98@lid", chatstore.Draft{Text: "only", Attachment: "/a/b.png"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeChat("98@lid", "31601@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Draft("31601@s.whatsapp.net"); d != (chatstore.Draft{Text: "only", Attachment: "/a/b.png"}) {
		t.Errorf("Draft = %+v; the lone Draft must move whole", d)
	}
}

func TestMergeChatFindsTheLIDInAnyTable(t *testing.T) {
	s, db := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "group@g.us", Kind: "group", LastActive: 5, Messages: []whatsapp.Message{msg("g1", 5)}}}})
	if changed, err := s.MergeChat(lidJID, phoneJID); err != nil || !changed {
		t.Fatalf("a LID that is only a group sender: MergeChat = %v, %v; want true", changed, err)
	}
	if n := rows(t, db, `SELECT COUNT(*) FROM messages WHERE sender_jid = ?`, phoneJID); n != 1 {
		t.Errorf("group sender was not rewritten (%d)", n)
	}
	if err := s.SetDraft("97@lid", chatstore.Draft{Text: "orphan"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.MergeChat("97@lid", "31602@s.whatsapp.net"); err != nil || !changed {
		t.Fatalf("an orphan Draft: MergeChat = %v, %v; want true", changed, err)
	}
	if d, _ := s.Draft("31602@s.whatsapp.net"); d.Text != "orphan" {
		t.Errorf("Draft = %+v", d)
	}
}

// A pin of App State is newer than a History Sync pin. A merge keeps the live pin, also an unpin.
func TestMergeChatKeepsTheLivePin(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: lidJID, LastActive: 50, Pinned: true, Messages: []whatsapp.Message{msg("a", 10)}},
		{JID: phoneJID, LastActive: 30, Messages: []whatsapp.Message{msg("b", 30)}},
	}})
	apply(t, s, whatsapp.Pin{JID: phoneJID, Pinned: false})
	if _, err := s.MergeChat(lidJID, phoneJID); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Chat(phoneJID); err != nil || c.Pinned {
		t.Errorf("Pinned = %v, %v; want the live unpin", c.Pinned, err)
	}
}

// A LID that is only a target of a System Message is listed, and merges, so the line shows the phone name.
func TestMergeChatRenamesASystemMessageTarget(t *testing.T) {
	s, _ := newStore(t)
	sys := whatsapp.Message{ID: "sys-1", Sender: "2@s.whatsapp.net", Time: 10, System: &whatsapp.System{Kind: whatsapp.SystemAdd, Targets: []string{lidJID}}}
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "g1@g.us", Kind: whatsapp.KindGroup, LastActive: 10, Messages: []whatsapp.Message{sys}}}})
	apply(t, s, whatsapp.ContactName{JID: phoneJID, Name: "Ann"})
	lids, err := s.LIDs()
	if err != nil || len(lids) != 1 || lids[0] != lidJID {
		t.Fatalf("LIDs = %v, %v; want [%s]", lids, err, lidJID)
	}
	if changed, err := s.MergeChat(lidJID, phoneJID); err != nil || !changed {
		t.Fatalf("MergeChat = %v, %v; want true", changed, err)
	}
	m, err := s.Message("g1@g.us", "sys-1")
	if err != nil || m.Text != "+2 added Ann" {
		t.Fatalf("Message = %q, %v; want %q", m.Text, err, "+2 added Ann")
	}
}
