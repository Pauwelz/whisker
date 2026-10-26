package chatstore_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// seedChat stores three Messages (times 10, 20, 30) of colGroup. Each has Media, a Reaction and a Recipient row.
// It returns the Media files that RemoveMedia deleted so far.
func seedChat(t *testing.T, s *chatstore.Store, db *sql.DB) *[]string {
	t.Helper()
	removed := &[]string{}
	s.RemoveMedia = func(chat, id string) { *removed = append(*removed, chat+":"+id) }
	for i, id := range []string{"m1", "m2", "m3"} {
		live(t, s, whatsapp.Message{ID: id, Sender: annJID, Time: int64(10 * (i + 1)), Text: id, Media: pic(id)})
		if _, err := s.SetReaction(colGroup, id, bobJID, false, "+"); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			`INSERT INTO recipients (chat_jid, id, phone, lid) VALUES (?, ?, 'p', 'l')`,
			`INSERT INTO group_receipts (chat_jid, id, sender, state) VALUES (?, ?, 's', 'delivered')`,
		} {
			if _, err := db.Exec(q, colGroup, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	return removed
}

func count(t *testing.T, db *sql.DB, query string, args ...any) (n int) {
	t.Helper()
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteMessageRemovesEveryRowOfTheMessage(t *testing.T) {
	s, db := newStore(t)
	removed := seedChat(t, s, db)
	for range 2 { // the second run changes nothing
		if err := s.DeleteMessage(colGroup, "m1"); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"messages", "media", "recipients", "group_receipts"} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+table+` WHERE chat_jid = ? AND id = 'm1'`, colGroup); n != 0 {
			t.Errorf("%s holds %d rows of the deleted Message", table, n)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM reactions WHERE chat_jid = ? AND message_id = 'm1'`, colGroup); n != 0 {
		t.Errorf("reactions holds %d rows of the deleted Message", n)
	}
	if !slices.Equal(*removed, []string{colGroup + ":m1"}) {
		t.Errorf("RemoveMedia calls = %v; want one", *removed)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM messages WHERE chat_jid = ?`, colGroup); n != 2 {
		t.Errorf("%d Messages left; want 2", n)
	}
}

func TestClearChatKeepsTheNewerMessagesAndTheChat(t *testing.T) {
	s, db := newStore(t)
	removed := seedChat(t, s, db)
	for i := range 2 {
		changed, err := s.ClearChat(colGroup, 20)
		if err != nil || changed != (i == 0) {
			t.Fatalf("ClearChat run %d = %v, %v; want %v, nil", i, changed, err, i == 0)
		}
	}
	if !slices.Equal(*removed, []string{colGroup + ":m1", colGroup + ":m2"}) {
		t.Errorf("RemoveMedia calls = %v; want m1 and m2", *removed)
	}
	var ids []string
	rows, _ := db.Query(`SELECT id FROM messages WHERE chat_jid = ?`, colGroup)
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if !slices.Equal(ids, []string{"m3"}) {
		t.Errorf("Messages left = %v; want m3", ids)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM reactions WHERE chat_jid = ?`, colGroup); n != 1 {
		t.Errorf("%d Reactions left; want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM chats WHERE jid = ?`, colGroup); n != 1 {
		t.Error("ClearChat removed the Chat")
	}
}

func TestClearChatWithoutATimestampClearsAllAndKeepsTheDraft(t *testing.T) {
	s, db := newStore(t)
	removed := seedChat(t, s, db)
	if err := s.SetDraft(colGroup, chatstore.Draft{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ClearChat(colGroup, 0); err != nil || !changed {
		t.Fatalf("ClearChat = %v, %v", changed, err)
	}
	if len(*removed) != 3 || count(t, db, `SELECT COUNT(*) FROM messages`) != 0 || count(t, db, `SELECT COUNT(*) FROM media`) != 0 {
		t.Errorf("removed %v; want all three Messages and their Media gone", *removed)
	}
	if d, _ := s.Draft(colGroup); d.Text != "hi" {
		t.Errorf("Draft = %+v; want it kept", d)
	}
}

// Every table with a Chat key or a Message key holds no row of the deleted Chat. The names stay.
func TestDeleteChatRemovesEveryRowOfTheChat(t *testing.T) {
	s, db := newStore(t)
	removed := seedChat(t, s, db)
	other := "other@g.us"
	if _, err := s.PutLive(whatsapp.NewMessage{Chat: other, Kind: whatsapp.KindGroup, Message: whatsapp.Message{ID: "o1", Sender: annJID, Time: 1, Text: "keep"}}, chatstore.LivePolicy{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraft(colGroup, chatstore.Draft{Text: "hi", Attachments: []string{"/tmp/a.png"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPicture(colGroup, "p1", []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	apply(t, s, whatsapp.PushName{JID: annJID, Name: "Ann"})

	keyed := map[string]string{} // table to its Chat column
	rows, err := db.Query(`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND (p.name = 'chat_jid' OR (p.name = 'jid' AND m.name IN ('chats', 'pictures')))`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, col string
		rows.Scan(&table, &col)
		keyed[table] = col
	}
	rows.Close()
	for _, table := range []string{"chats", "messages", "media", "reactions", "drafts", "pictures", "recipients", "group_receipts"} {
		col, ok := keyed[table]
		if !ok {
			t.Fatalf("table %s has no Chat key; keyed = %v", table, keyed)
		}
		if table == "reactions" {
			col = "chat_jid"
		}
		if count(t, db, `SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, colGroup) == 0 {
			t.Fatalf("the test left %s empty", table)
		}
	}

	for i := range 2 {
		attachments, gone, err := s.DeleteChat(colGroup)
		if err != nil || gone != (i == 0) || (i == 0) != slices.Equal(attachments, []string{"/tmp/a.png"}) {
			t.Fatalf("DeleteChat run %d = %v, %v, %v", i, attachments, gone, err)
		}
	}
	for table, col := range keyed {
		if n := count(t, db, `SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, colGroup); n != 0 {
			t.Errorf("%s still holds %d rows of the deleted Chat", table, n)
		}
	}
	if len(*removed) != 3 {
		t.Errorf("RemoveMedia calls = %v; want 3", *removed)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM names`); n == 0 {
		t.Error("DeleteChat removed the names")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM messages WHERE chat_jid = ?`, other); n != 1 {
		t.Error("DeleteChat touched another Chat")
	}
}

func TestHasMessageAfter(t *testing.T) {
	s, db := newStore(t)
	seedChat(t, s, db)
	for until, want := range map[int64]bool{20: true, 29: true, 30: false} {
		if got, err := s.HasMessageAfter(colGroup, until); err != nil || got != want {
			t.Errorf("HasMessageAfter(%d) = %v, %v; want %v", until, got, err, want)
		}
	}
}
