package chatstore_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

var now = time.Unix(1_000_000, 0)

func newStore(t *testing.T) (*chatstore.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := chatstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return chatstore.New(db, func() time.Time { return now }), db
}

func apply(t *testing.T, s *chatstore.Store, ev whatsapp.Event) {
	t.Helper()
	if err := s.Apply(ev); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, s *chatstore.Store, offset, limit int) chatstore.Page {
	t.Helper()
	page, err := s.List(offset, limit, "")
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestMigrateIsIdempotentAndSetsTheVersion(t *testing.T) {
	_, db := newStore(t)
	if err := chatstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v < 1 {
		t.Errorf("user_version = %d, %v; want at least 1", v, err)
	}
}

func TestMigrateRefusesANewerStore(t *testing.T) {
	_, db := newStore(t)
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := chatstore.Migrate(db); err == nil {
		t.Error("Migrate accepted a store from a newer Daemon")
	}
}

func TestNameRule(t *testing.T) {
	s, _ := newStore(t)
	chats := []whatsapp.HistoryChat{
		{JID: "1111@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 10},
		{JID: "2222@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 9},
		{JID: "3333@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 8},
		{JID: "4444-5@g.us", Kind: whatsapp.KindGroup, Subject: "Family", LastActive: 7},
	}
	apply(t, s, whatsapp.HistorySync{Chats: chats})
	apply(t, s, whatsapp.PushName{JID: "1111@s.whatsapp.net", Name: "Ann (push)"})
	apply(t, s, whatsapp.ContactName{JID: "1111@s.whatsapp.net", Name: "Ann"})
	apply(t, s, whatsapp.PushName{JID: "2222@s.whatsapp.net", Name: "Bob (push)"})

	want := []string{"Ann", "Bob (push)", "+3333", "Family"}
	for i, c := range list(t, s, 0, 10).Chats {
		if c.Name != want[i] {
			t.Errorf("chat %d name = %q, want %q", i, c.Name, want[i])
		}
	}

	apply(t, s, whatsapp.ContactName{JID: "1111@s.whatsapp.net", Name: ""}) // the contact is removed
	if got := list(t, s, 0, 10).Chats[0].Name; got != "Ann (push)" {
		t.Errorf("name after contact removal = %q, want the Push Name", got)
	}
}

func TestHistoryPushNamesAndMessagePushNamesFeedTheRule(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{
		PushNames: []whatsapp.PushName{{JID: "1111@s.whatsapp.net", Name: "Ann"}},
		Chats: []whatsapp.HistoryChat{
			{JID: "1111@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 2},
			{JID: "2222@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 1, Messages: []whatsapp.Message{
				{ID: "m1", Sender: "2222@s.whatsapp.net", Time: 1, Text: "hi", PushName: "Bob"},
			}},
		},
	})
	got := list(t, s, 0, 10).Chats
	if got[0].Name != "Ann" || got[1].Name != "Bob" {
		t.Errorf("names = %q, %q; want Ann, Bob", got[0].Name, got[1].Name)
	}
}

func TestOrderPinnedFirstThenNewest(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: "a@s.whatsapp.net", LastActive: 1, Pinned: true},
		{JID: "b@s.whatsapp.net", LastActive: 5},
		{JID: "c@s.whatsapp.net", LastActive: 9},
		{JID: "d@s.whatsapp.net", LastActive: 2, Pinned: true},
	}})
	var got []string
	for _, c := range list(t, s, 0, 10).Chats {
		got = append(got, c.JID[:1])
	}
	if want := "dacb"; join(got) != want {
		t.Errorf("order = %s, want %s", join(got), want)
	}
	apply(t, s, whatsapp.Pin{JID: "a@s.whatsapp.net", Pinned: false})
	if first := list(t, s, 0, 10).Chats[0].JID; first != "d@s.whatsapp.net" {
		t.Errorf("first after unpin = %s, want d", first)
	}
}

func join(s []string) string {
	out := ""
	for _, x := range s {
		out += x
	}
	return out
}

func TestMutedAndReadMarkerFromAppState(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: "a@s.whatsapp.net", LastActive: 4, MutedUntil: -1, Unread: true},
		{JID: "b@s.whatsapp.net", LastActive: 3, MutedUntil: now.Unix() + 60},
		{JID: "c@s.whatsapp.net", LastActive: 2, MutedUntil: now.Unix() - 60}, // expired
		{JID: "d@s.whatsapp.net", LastActive: 1},
	}})
	chats := list(t, s, 0, 10).Chats
	muted := []bool{true, true, false, false}
	for i, c := range chats {
		if c.Muted != muted[i] {
			t.Errorf("%s muted = %v, want %v", c.JID, c.Muted, muted[i])
		}
	}
	if !chats[0].Unread || chats[3].Unread {
		t.Error("unread marks do not match the history")
	}
	apply(t, s, whatsapp.ReadMarker{JID: "a@s.whatsapp.net", Unread: false})
	apply(t, s, whatsapp.ReadMarker{JID: "d@s.whatsapp.net", Unread: true})
	apply(t, s, whatsapp.Mute{JID: "d@s.whatsapp.net", Until: -1})
	apply(t, s, whatsapp.Mute{JID: "a@s.whatsapp.net", Until: 0})
	chats = list(t, s, 0, 10).Chats
	if chats[0].Unread || chats[0].Muted || !chats[3].Unread || !chats[3].Muted {
		t.Errorf("App State changes did not apply: %+v", chats)
	}
}

func TestAppStateBeforeHistoryDoesNotCreateAnEmptyChat(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.Pin{JID: "a@s.whatsapp.net", Pinned: true})
	if n := len(list(t, s, 0, 10).Chats); n != 0 {
		t.Fatalf("chats = %d, want 0", n)
	}
}

func TestPagesAndRepeatedBatches(t *testing.T) {
	s, db := newStore(t)
	var chats []whatsapp.HistoryChat
	for i := 1; i <= 5; i++ {
		chats = append(chats, whatsapp.HistoryChat{JID: string(rune('a'+i)) + "@s.whatsapp.net", LastActive: int64(i)})
	}
	apply(t, s, whatsapp.HistorySync{Chats: chats})
	apply(t, s, whatsapp.HistorySync{Chats: chats[:1]}) // the same Chat again must not duplicate
	p1 := list(t, s, 0, 2)
	p3 := list(t, s, 4, 2)
	if len(p1.Chats) != 2 || !p1.More || len(p3.Chats) != 1 || p3.More {
		t.Errorf("pages = %d more=%v, %d more=%v", len(p1.Chats), p1.More, len(p3.Chats), p3.More)
	}
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "x@s.whatsapp.net", LastActive: 1,
		Messages: []whatsapp.Message{{ID: "1", Sender: "x@s.whatsapp.net", Text: "a"}}}}})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "x@s.whatsapp.net", LastActive: 1,
		Messages: []whatsapp.Message{{ID: "1", Sender: "x@s.whatsapp.net", Text: "a"}, {ID: "2", Sender: "x@s.whatsapp.net", Text: "b"}}}}})
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&n); err != nil || n != 2 {
		t.Errorf("messages = %d, %v; want 2", n, err)
	}
}

func TestSearchMatchesChatNameAndParticipantName(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{
		PushNames: []whatsapp.PushName{{JID: "2@s.whatsapp.net", Name: "Bob Push"}},
		Chats: []whatsapp.HistoryChat{
			{JID: "1@s.whatsapp.net", LastActive: 40},
			{JID: "g1@g.us", Kind: whatsapp.KindGroup, Subject: "Chess Club", LastActive: 30,
				Messages: []whatsapp.Message{{ID: "m1", Sender: "2@s.whatsapp.net", Time: 30}}},
			{JID: "g2@g.us", Kind: whatsapp.KindGroup, Subject: "Family", LastActive: 20},
		},
	})
	apply(t, s, whatsapp.ContactName{JID: "1@s.whatsapp.net", Name: "Ann Contact"})
	search := func(q string) []string {
		t.Helper()
		page, err := s.List(0, 50, q)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, c := range page.Chats {
			names = append(names, c.Name)
		}
		return names
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"ann", []string{"Ann Contact"}},    // Chat name, case does not matter
		{" CHESS ", []string{"Chess Club"}}, // Group subject, query trimmed
		{"bob", []string{"Chess Club"}},     // Participant who wrote in a Group Chat
		{"+1", []string{}},                  // the contact name hides the phone number
		{"nobody", []string{}},
		{"", []string{"Ann Contact", "Chess Club", "Family"}},
	} {
		if got := search(tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("search %q = %v, want %v", tc.query, got, tc.want)
		}
	}
	page, _ := s.List(1, 1, "c") // "Ann Contact" and "Chess Club" both hold "c"
	if len(page.Chats) != 1 || page.Chats[0].Name != "Chess Club" || page.More {
		t.Errorf("paged search = %+v", page)
	}
}
