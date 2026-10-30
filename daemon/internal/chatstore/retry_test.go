package chatstore_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

func addRetry(t *testing.T, s *chatstore.Store, chat, id, target string) {
	t.Helper()
	if err := s.AddRetry(chat, id, target, "wa", []byte(id)); err != nil {
		t.Fatal(err)
	}
}

func retryIDs(t *testing.T, db *sql.DB) int {
	t.Helper()
	return count(t, db, `SELECT COUNT(*) FROM retry_messages`)
}

func TestRetryFindsAMessageByChatOrAltChatAndReplacesOnRepeat(t *testing.T) {
	s, _ := newStore(t)
	if err := s.AddRetry(lidJID, "a", "", "wa", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRetry(lidJID, "a", "", "fb", []byte("two")); err != nil {
		t.Fatal(err)
	}
	for _, chat := range [][2]string{{lidJID, ""}, {phoneJID, lidJID}, {lidJID, phoneJID}} {
		format, data, err := s.Retry(chat[0], chat[1], "a")
		if err != nil || format != "fb" || string(data) != "two" {
			t.Errorf("Retry(%v) = %q, %q, %v; want fb, two, nil", chat, format, data, err)
		}
	}
}

func TestRetryOfAMissingMessageIsNoRows(t *testing.T) {
	s, _ := newStore(t)
	addRetry(t, s, lidJID, "a", "")
	for name, tc := range map[string][3]string{"other id": {lidJID, "", "b"}, "other chat": {phoneJID, "", "a"}} {
		format, data, err := s.Retry(tc[0], tc[1], tc[2])
		if !errors.Is(err, sql.ErrNoRows) || format != "" || data != nil {
			t.Errorf("%s: Retry = %q, %q, %v; want empty values and sql.ErrNoRows", name, format, data, err)
		}
	}
}

func TestExpireDeletesRetriesOlderThanSevenDays(t *testing.T) {
	s, db := newStore(t)
	for id, age := range map[string]time.Duration{"old": 7*24*time.Hour + time.Minute, "new": 7*24*time.Hour - time.Minute} {
		if _, err := db.Exec(`INSERT INTO retry_messages (chat_jid, id, format, data, timestamp) VALUES (?, ?, 'wa', x'00', ?)`, phoneJID, id, now.Add(-age).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Expire(now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Retry(phoneJID, "", "new"); err != nil {
		t.Errorf("new row: %v; want it kept", err)
	}
	if _, _, err := s.Retry(phoneJID, "", "old"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("old row: %v; want it deleted", err)
	}
}

// seedRetries stores the Message m1, an edit and a reaction that act on it, and the unrelated Message other.
// The rows are under the LID, which differs from the Chat of the Message.
func seedRetries(t *testing.T, s *chatstore.Store) {
	t.Helper()
	addRetry(t, s, lidJID, "m1", "")
	addRetry(t, s, lidJID, "edit", "m1")
	addRetry(t, s, lidJID, "react", "m1")
	addRetry(t, s, lidJID, "other", "")
}

func wantOnlyOther(t *testing.T, db *sql.DB) {
	t.Helper()
	if n := retryIDs(t, db); n != 1 || count(t, db, `SELECT COUNT(*) FROM retry_messages WHERE id = 'other'`) != 1 {
		t.Errorf("retry_messages holds %d rows; want only the unrelated one", n)
	}
}

func TestDeleteMessageDeletesItsRetryRowsAndTheOnesThatActOnIt(t *testing.T) {
	s, db := newStore(t)
	live(t, s, whatsapp.Message{ID: "m1", Sender: annJID, Time: 1, Text: "x"})
	seedRetries(t, s)
	if err := s.DeleteMessage(colGroup, "m1"); err != nil {
		t.Fatal(err)
	}
	wantOnlyOther(t, db)
}

func TestRevokeDeletesItsRetryRows(t *testing.T) {
	for name, revoke := range map[string]func(*chatstore.Store) (bool, error){
		"author": func(s *chatstore.Store) (bool, error) { return s.Revoke(colGroup, "m1", annJID, false) },
		"admin":  func(s *chatstore.Store) (bool, error) { return s.RevokeByAdmin(colGroup, "m1", annJID, false) },
	} {
		t.Run(name, func(t *testing.T) {
			s, db := newStore(t)
			live(t, s, whatsapp.Message{ID: "m1", Sender: annJID, Time: 1, Text: "x"})
			seedRetries(t, s)
			if changed, err := revoke(s); err != nil || !changed {
				t.Fatalf("revoke = %v, %v; want true, nil", changed, err)
			}
			wantOnlyOther(t, db)
		})
	}
}

func TestARevokeThatChangesNothingKeepsTheRetryRows(t *testing.T) {
	s, db := newStore(t)
	live(t, s, whatsapp.Message{ID: "m1", Sender: annJID, Time: 1, Text: "x"})
	seedRetries(t, s)
	if changed, err := s.Revoke(colGroup, "m1", bobJID, false); err != nil || changed {
		t.Fatalf("Revoke = %v, %v; want false, nil", changed, err)
	}
	if n := retryIDs(t, db); n != 4 {
		t.Errorf("retry_messages holds %d rows; want 4", n)
	}
}

func TestExpireDeletesTheRetryRowsOfAnExpiredMessage(t *testing.T) {
	s, db := newStore(t)
	live(t, s, whatsapp.Message{ID: "m1", Sender: annJID, Time: 1, Text: "x"})
	if _, err := db.Exec(`UPDATE messages SET expires_at = 50`); err != nil {
		t.Fatal(err)
	}
	seedRetries(t, s)
	if _, err := s.Expire(100); err != nil {
		t.Fatal(err)
	}
	wantOnlyOther(t, db)
}

func TestClearChatDeletesTheRetryRowsOfTheClearedMessages(t *testing.T) {
	s, db := newStore(t)
	seedChat(t, s, db)
	addRetry(t, s, lidJID, "m1", "")
	addRetry(t, s, lidJID, "edit", "m2")
	addRetry(t, s, lidJID, "m3", "")
	if _, err := s.ClearChat(colGroup, 20); err != nil { // m1 and m2 go
		t.Fatal(err)
	}
	if _, _, err := s.Retry(lidJID, "", "m3"); err != nil {
		t.Errorf("m3: %v; want it kept", err)
	}
	if n := retryIDs(t, db); n != 1 {
		t.Errorf("retry_messages holds %d rows; want 1", n)
	}
}

func TestDeleteChatDeletesItsRetryRows(t *testing.T) {
	s, db := newStore(t)
	seedChat(t, s, db)
	addRetry(t, s, lidJID, "m2", "")
	addRetry(t, s, colGroup, "orphan", "")
	addRetry(t, s, lidJID, "other", "")
	if _, _, err := s.DeleteChat(colGroup); err != nil {
		t.Fatal(err)
	}
	wantOnlyOther(t, db)
}
