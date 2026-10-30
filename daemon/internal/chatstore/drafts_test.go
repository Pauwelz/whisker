package chatstore_test

import (
	"reflect"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
)

func TestDraftKeepsItsAttachmentsInOrder(t *testing.T) {
	s, _ := newStore(t)
	want := chatstore.Draft{Text: "hi", Attachments: []string{"/b.png", "/a.pdf"}}
	if err := s.SetDraft("a@s.whatsapp.net", want); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Draft("a@s.whatsapp.net"); !reflect.DeepEqual(d, want) {
		t.Errorf("Draft = %+v, want %+v", d, want)
	}
}

func TestDraftWithoutAttachmentsHasAnEmptyList(t *testing.T) {
	s, _ := newStore(t)
	if d, _ := s.Draft("none@s.whatsapp.net"); d.Attachments == nil || len(d.Attachments) != 0 {
		t.Errorf("Attachments = %#v, want an empty list", d.Attachments)
	}
	if err := s.SetDraft("a@s.whatsapp.net", chatstore.Draft{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Draft("a@s.whatsapp.net"); d.Attachments == nil {
		t.Error("Attachments = nil, want an empty list")
	}
}

func TestEmptyDraftIsDeleted(t *testing.T) {
	s, db := newStore(t)
	s.SetDraft("a@s.whatsapp.net", chatstore.Draft{Attachments: []string{"/a"}})
	s.SetDraft("a@s.whatsapp.net", chatstore.Draft{Attachments: []string{}})
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM drafts`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Draft rows, want none", n)
	}
}

func TestMigrateTurnsAnAttachmentPathIntoAList(t *testing.T) {
	s, db := newStore(t)
	for _, q := range []string{
		`DROP TABLE drafts`,
		`CREATE TABLE drafts (chat_jid TEXT NOT NULL PRIMARY KEY, text TEXT NOT NULL, quote_id TEXT NOT NULL, edit_id TEXT NOT NULL, attachment TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO drafts VALUES ('one@s.whatsapp.net', 'a', '', '', '/x/y.png'), ('none@s.whatsapp.net', 'b', '', '', '')`,
		`ALTER TABLE messages DROP COLUMN in_unread_count`, // undo the steps after version 20 that Migrate runs again
		`ALTER TABLE messages DROP COLUMN revoked_by_admin`,
		`DROP TABLE retry_messages`,
		`PRAGMA user_version = 20`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := chatstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Draft("one@s.whatsapp.net"); !reflect.DeepEqual(d.Attachments, []string{"/x/y.png"}) {
		t.Errorf("Attachments = %#v, want one path", d.Attachments)
	}
	if d, _ := s.Draft("none@s.whatsapp.net"); d.Text != "b" || len(d.Attachments) != 0 {
		t.Errorf("Draft = %+v, want no Attachments", d)
	}
}
