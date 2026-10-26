package chatstore_test

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
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
	if _, err := s.Apply(ev); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, s *chatstore.Store, offset, limit int) chatstore.Page {
	t.Helper()
	page, err := s.List(offset, limit, "", false)
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

// The bar count covers the whole chat store: unread Chats that are not Muted.
func TestUnreadChatsCountsUnmutedUnreadChats(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: "a@s.whatsapp.net", LastActive: 4, Unread: true},
		{JID: "b@s.whatsapp.net", LastActive: 3, Unread: true, MutedUntil: -1},
		{JID: "c@s.whatsapp.net", LastActive: 2, Unread: true, MutedUntil: now.Unix() - 60}, // mute ended
		{JID: "d@s.whatsapp.net", LastActive: 1},
	}})
	if n, err := s.UnreadChats(); err != nil || n != 2 {
		t.Errorf("UnreadChats = %d, %v; want 2", n, err)
	}
}

// A late History Sync batch is older than the live App State. It must not undo it.
func TestHistorySyncKeepsFlagsOfAnExistingChat(t *testing.T) {
	s, _ := newStore(t)
	const jid = "a@s.whatsapp.net"
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1}}})
	apply(t, s, whatsapp.Pin{JID: jid, Pinned: true})
	apply(t, s, whatsapp.Mute{JID: jid, Until: -1})
	apply(t, s, whatsapp.ReadMarker{JID: jid, Unread: true})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1}}}) // stale flags
	c := list(t, s, 0, 10).Chats[0]
	if !c.Pinned || !c.Muted || !c.Unread {
		t.Errorf("flags after a late batch = %+v, want all kept", c)
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
		page, err := s.List(0, 50, q, false)
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
	page, _ := s.List(1, 1, "c", false) // "Ann Contact" and "Chess Club" both hold "c"
	if len(page.Chats) != 1 || page.Chats[0].Name != "Chess Club" || page.More {
		t.Errorf("paged search = %+v", page)
	}
}

// The store writes the policy it is given: no policy of its own.
func TestPutLiveWritesTheReadMarkerOnlyWhenTold(t *testing.T) {
	s, _ := newStore(t)
	live := func(chat, id string, p chatstore.LivePolicy) {
		t.Helper()
		ev := whatsapp.NewMessage{Chat: chat, Message: whatsapp.Message{ID: id, Sender: chat, Time: 5, Text: "hi"}}
		if err := s.PutLive(ev, p); err != nil {
			t.Fatal(err)
		}
	}
	live("a@s.whatsapp.net", "1", chatstore.LivePolicy{MediaState: chatstore.MediaNone})
	live("b@s.whatsapp.net", "1", chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: true})
	unread := map[string]bool{}
	for _, c := range list(t, s, 0, 10).Chats {
		unread[c.JID] = c.Unread
	}
	if unread["a@s.whatsapp.net"] || !unread["b@s.whatsapp.net"] {
		t.Errorf("unread marks = %v", unread)
	}
	if _, err := s.ClearUnread("b@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	live("b@s.whatsapp.net", "1", chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: true}) // a repeat
	if list(t, s, 0, 10).Chats[0].Unread || list(t, s, 0, 10).Chats[1].Unread {
		t.Error("a repeated Message must not set the Read Marker again")
	}
}

func TestMentionsUseTheNameRuleAndRevokeClearsThem(t *testing.T) {
	s, _ := newStore(t)
	if err := s.PutLive(whatsapp.NewMessage{Chat: "g@g.us", Kind: whatsapp.KindGroup, Message: whatsapp.Message{
		ID: "m1", Sender: "2@s.whatsapp.net", Time: 5, Text: "@3 and @4",
		Mentions: []string{"3@s.whatsapp.net", "4@s.whatsapp.net"}}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	apply(t, s, whatsapp.PushName{JID: "3@s.whatsapp.net", Name: "Zed"})
	m, err := s.Message("g@g.us", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "@Zed and @+4" || !slices.Equal(m.Mentions, []string{"@Zed", "@+4"}) {
		t.Fatalf("got %q %v", m.Text, m.Mentions)
	}
	if _, err := s.Revoke("g@g.us", "m1"); err != nil {
		t.Fatal(err)
	}
	if m, _ = s.Message("g@g.us", "m1"); m.Text != "" || len(m.Mentions) != 0 {
		t.Fatalf("a Tombstone keeps no mentions: %q %v", m.Text, m.Mentions)
	}
}

const syncJID = "1111@s.whatsapp.net"

func history(pinned bool, muted int64, unread bool, timer int64) whatsapp.HistorySync {
	return whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: syncJID, LastActive: 5, Pinned: pinned, MutedUntil: muted, Unread: unread, Timer: timer}}}
}

func TestHistorySyncFillsFlagsOfAChatThatALiveMessageCreated(t *testing.T) {
	s, _ := newStore(t)
	if err := s.PutLive(whatsapp.NewMessage{Chat: syncJID, Message: whatsapp.Message{ID: "m1", Time: 5, Text: "hi"}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	apply(t, s, history(true, -1, true, 60))
	c, err := s.Chat(syncJID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Pinned || !c.Muted || !c.Unread || c.Timer != 60 {
		t.Errorf("chat = %+v; want pinned, muted, unread, timer 60 from History Sync", c)
	}
}

func TestLiveFlagsSurviveALateHistorySync(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, history(false, 0, true, 0))
	apply(t, s, whatsapp.Pin{JID: syncJID, Pinned: true})
	apply(t, s, whatsapp.Mute{JID: syncJID, Until: -1})
	apply(t, s, whatsapp.ReadMarker{JID: syncJID, Unread: false})
	apply(t, s, whatsapp.ChatTimer{Chat: syncJID, Seconds: 30})
	apply(t, s, history(false, 0, true, 0))
	c, err := s.Chat(syncJID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Pinned || !c.Muted || c.Unread || c.Timer != 30 {
		t.Errorf("chat = %+v; want the live values: pinned, muted, read, timer 30", c)
	}
}

func TestLiveMessageUnreadStaysOverALateHistorySync(t *testing.T) {
	s, _ := newStore(t)
	if err := s.PutLive(whatsapp.NewMessage{Chat: syncJID, Message: whatsapp.Message{ID: "m1", Time: 5}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: true}); err != nil {
		t.Fatal(err)
	}
	apply(t, s, history(false, 0, false, 0))
	if c, _ := s.Chat(syncJID); !c.Unread {
		t.Error("a live unread Chat turned read by a late History Sync")
	}
}

func TestListCountsWaitingMessagesAndFlagsMentions(t *testing.T) {
	s, _ := newStore(t)
	live := func(id string, mentionsMe bool) {
		t.Helper()
		ev := whatsapp.NewMessage{Chat: "g@g.us", Kind: whatsapp.KindGroup, Message: whatsapp.Message{ID: id, Sender: "b@s.whatsapp.net", Time: 5, Text: "hi", MentionsMe: mentionsMe}}
		if err := s.PutLive(ev, chatstore.LivePolicy{MediaState: chatstore.MediaNone, MarkUnread: true}); err != nil {
			t.Fatal(err)
		}
	}
	live("1", false)
	live("2", true)
	live("3", false)
	if c := list(t, s, 0, 10).Chats[0]; c.UnreadCount != 3 || !c.Mention {
		t.Errorf("unreadCount = %d, mention = %v, want 3, true", c.UnreadCount, c.Mention)
	}
	if _, err := s.ClearUnread("g@g.us"); err != nil {
		t.Fatal(err)
	}
	if c := list(t, s, 0, 10).Chats[0]; c.UnreadCount != 0 || c.Mention {
		t.Errorf("a read Chat has unreadCount = %d, mention = %v", c.UnreadCount, c.Mention)
	}
}

// An older Message of the user keeps the Delivery State that History Sync carries. A later batch moves it forward, never back.
func TestHistorySyncSetsTheDeliveryStateOfOwnMessages(t *testing.T) {
	s, _ := newStore(t)
	const jid = "a@s.whatsapp.net"
	batch := func(state string) whatsapp.HistorySync {
		return whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1, Messages: []whatsapp.Message{
			{ID: "m1", FromMe: true, Time: 1, Text: "hi", State: state},
		}}}}
	}
	state := func() string {
		t.Helper()
		m, err := s.Message(jid, "m1")
		if err != nil {
			t.Fatal(err)
		}
		return m.State
	}
	apply(t, s, batch(""))
	if got := state(); got != whatsapp.StateSent {
		t.Fatalf("no state from History Sync: State = %q, want sent", got)
	}
	apply(t, s, batch(whatsapp.StateRead))
	if got := state(); got != whatsapp.StateRead {
		t.Errorf("a later batch with read: State = %q, want read", got)
	}
	apply(t, s, batch(whatsapp.StateDelivered))
	if got := state(); got != whatsapp.StateRead {
		t.Errorf("a stale batch moved the state back: State = %q, want read", got)
	}
}

// A later History Sync copy of a Message fills a thumbnail or thumbnail link that the first copy lacked.
// What is stored already stays.
func TestALaterCopyFillsAMissingThumbnail(t *testing.T) {
	s, _ := newStore(t)
	const jid = "a@s.whatsapp.net"
	batch := func(md whatsapp.Media) whatsapp.HistorySync {
		md.Kind, md.Key = media.Image, make([]byte, 32)
		return whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1, Messages: []whatsapp.Message{
			{ID: "m1", Time: 1, Media: &md},
		}}}}
	}
	ref := func() chatstore.MediaRef {
		t.Helper()
		r, err := s.MediaOf(jid, "m1")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	apply(t, s, batch(whatsapp.Media{}))
	apply(t, s, batch(whatsapp.Media{Thumbnail: []byte("a"), ThumbDirectPath: "/t1", ThumbSHA256: []byte{1}, ThumbEncSHA256: []byte{2}}))
	if r := ref(); string(r.Thumbnail) != "a" || r.ThumbDirectPath != "/t1" || r.ThumbSHA256[0] != 1 || r.ThumbEncSHA256[0] != 2 {
		t.Fatalf("a later copy did not fill the gaps: %+v", r.Media)
	}
	apply(t, s, batch(whatsapp.Media{Thumbnail: []byte("b"), ThumbDirectPath: "/t2"}))
	if r := ref(); string(r.Thumbnail) != "a" || r.ThumbDirectPath != "/t1" {
		t.Errorf("a later copy replaced what was stored: %+v", r.Media)
	}
}

// The first App State sync can come before the History Sync batch that holds the Chat. The batch carries a stale pin.
// The pin of App State must win, also for a Chat that is not stored yet.
func TestAPinBeforeHistorySyncWins(t *testing.T) {
	s, _ := newStore(t)
	const pinned, unpinned = "a@s.whatsapp.net", "b@s.whatsapp.net"
	apply(t, s, whatsapp.Pin{JID: pinned, Pinned: true})
	apply(t, s, whatsapp.Pin{JID: unpinned, Pinned: false})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: pinned, LastActive: 1, Pinned: false},
		{JID: unpinned, LastActive: 2, Pinned: true},
	}})
	for _, jid := range []string{pinned, unpinned} {
		c, err := s.Chat(jid)
		if err != nil {
			t.Fatal(err)
		}
		if want := jid == pinned; c.Pinned != want {
			t.Errorf("%s: Pinned = %v, want %v (the App State pin)", jid, c.Pinned, want)
		}
	}
}

// The flags of one full App State sync come as one batch, so the Plugin gets one chatsChanged, not one per Chat.
func TestAppStateBatchAppliesEveryFlag(t *testing.T) {
	s, _ := newStore(t)
	const a, b = "a@s.whatsapp.net", "b@s.whatsapp.net"
	apply(t, s, whatsapp.AppStateBatch{Events: []whatsapp.Event{
		whatsapp.Pin{JID: a, Pinned: true},
		whatsapp.Mute{JID: b, Until: -1},
		whatsapp.ReadMarker{JID: b, Unread: true},
	}})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: a, LastActive: 1}, {JID: b, LastActive: 2}}})
	ca, _ := s.Chat(a)
	cb, _ := s.Chat(b)
	if !ca.Pinned || !cb.Muted || !cb.Unread {
		t.Errorf("a = %+v, b = %+v; want a pinned, b muted and unread", ca, cb)
	}
}

// A Pin can store a Chat before any Message. The first live Message sets its kind.
func TestALiveMessageSetsTheKindOfAChatThatAPinStored(t *testing.T) {
	s, _ := newStore(t)
	const g = "g@g.us"
	apply(t, s, whatsapp.Pin{JID: g, Pinned: true})
	ev := whatsapp.NewMessage{Chat: g, Kind: whatsapp.KindGroup, Message: whatsapp.Message{ID: "m1", Sender: "b@s.whatsapp.net", Time: 5, Text: "hi"}}
	if err := s.PutLive(ev, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Chat(g); err != nil || c.Kind != whatsapp.KindGroup {
		t.Errorf("Kind = %q, %v; want group", c.Kind, err)
	}
}

func listArchived(t *testing.T, s *chatstore.Store, query string, archived bool) []string {
	t.Helper()
	page, err := s.List(0, 50, query, archived)
	if err != nil {
		t.Fatal(err)
	}
	jids := []string{}
	for _, c := range page.Chats {
		jids = append(jids, c.JID)
	}
	return jids
}

func TestArchivedChatsLeaveTheMainListButSearchFindsThem(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: "a@s.whatsapp.net", LastActive: 2, Unread: true},
		{JID: "b@s.whatsapp.net", LastActive: 1, Unread: true, Archived: true},
	}})
	if got := listArchived(t, s, "", false); !slices.Equal(got, []string{"a@s.whatsapp.net"}) {
		t.Errorf("main list = %v", got)
	}
	if got := listArchived(t, s, "", true); !slices.Equal(got, []string{"b@s.whatsapp.net"}) {
		t.Errorf("archived list = %v", got)
	}
	if got := listArchived(t, s, "+", false); len(got) != 2 {
		t.Errorf("search = %v; want both Chats", got)
	}
	if got := listArchived(t, s, "+", true); !slices.Equal(got, []string{"b@s.whatsapp.net"}) {
		t.Errorf("archived search = %v", got)
	}
	if n, err := s.UnreadChats(); err != nil || n != 1 {
		t.Errorf("UnreadChats = %d, %v; want 1 (archived Chats do not count)", n, err)
	}
	apply(t, s, whatsapp.Archive{JID: "b@s.whatsapp.net", Archived: false})
	if got := listArchived(t, s, "", false); len(got) != 2 {
		t.Errorf("main list after unarchive = %v", got)
	}
}

func TestALiveArchiveSurvivesALateHistorySync(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", LastActive: 1, Archived: true}}})
	apply(t, s, whatsapp.Archive{JID: "a@s.whatsapp.net", Archived: false})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", LastActive: 1, Archived: true}}})
	if got := listArchived(t, s, "", false); len(got) != 1 {
		t.Errorf("main list = %v; want the live unarchive to stay", got)
	}
}

func TestANewMessageUnarchivesOnlyWhenTheSettingIsOn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting []whatsapp.Event
		want    bool // archived after the Message
	}{
		{"unknown", nil, true},
		{"off", []whatsapp.Event{whatsapp.UnarchiveChats{On: false}}, true},
		{"on", []whatsapp.Event{whatsapp.UnarchiveChats{On: true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", LastActive: 1, Archived: true}}})
			for _, ev := range tc.setting {
				apply(t, s, ev)
			}
			ev := whatsapp.NewMessage{Chat: "a@s.whatsapp.net", Message: whatsapp.Message{ID: "1", Sender: "a@s.whatsapp.net", Time: 5, Text: "hi"}}
			if err := s.PutLive(ev, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
				t.Fatal(err)
			}
			c, err := s.Chat("a@s.whatsapp.net")
			if err != nil || c.Archived != tc.want {
				t.Errorf("archived = %v, %v; want %v", c.Archived, err, tc.want)
			}
			if !tc.want { // a late History Sync must not archive it again
				apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", LastActive: 1, Archived: true}}})
				if c, _ := s.Chat("a@s.whatsapp.net"); c.Archived {
					t.Error("History Sync archived the Chat again")
				}
			}
		})
	}
}

func TestOldestMessageIsTheAnchorOfTheChat(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.OldestMessage("a@s.whatsapp.net"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a Chat with no Message has no anchor, got %v", err)
	}
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", Messages: []whatsapp.Message{
		{ID: "new", Time: 300}, {ID: "old", FromMe: true, Time: 100}, {ID: "mid", Time: 200},
	}}}})
	got, err := s.OldestMessage("a@s.whatsapp.net")
	if err != nil || got != (whatsapp.HistoryAnchor{ID: "old", FromMe: true, Time: 100}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	// A System Message can carry an ID that the Daemon made, which the phone does not know.
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", Messages: []whatsapp.Message{
		{ID: "sys", Time: 50, System: &whatsapp.System{Kind: whatsapp.SystemTimer, Value: "86400"}},
	}}}})
	if got, err := s.OldestMessage("a@s.whatsapp.net"); err != nil || got.ID != "old" {
		t.Fatalf("a System Message is no anchor, got %+v, %v", got, err)
	}
}

func TestApplyHistoryCountsOnlyNewMessages(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "a@s.whatsapp.net", Messages: []whatsapp.Message{{ID: "2", Time: 200}}}}})
	added, err := s.ApplyHistory(whatsapp.HistorySync{OnDemand: true, Chats: []whatsapp.HistoryChat{
		{JID: "a@s.whatsapp.net", Messages: []whatsapp.Message{{ID: "1", Time: 100}, {ID: "2", Time: 200}}},
		{JID: "b@s.whatsapp.net"},
	}})
	if err != nil || added["a@s.whatsapp.net"] != 1 || added["b@s.whatsapp.net"] != 0 {
		t.Fatalf("got %v, %v", added, err)
	}
}

func TestMentionsOfNearNumbersEachGetTheirName(t *testing.T) {
	s, _ := newStore(t)
	if err := s.PutLive(whatsapp.NewMessage{Chat: "g@g.us", Kind: whatsapp.KindGroup, Message: whatsapp.Message{
		ID: "m1", Sender: "2@s.whatsapp.net", Time: 5, Text: "@12 and @123",
		Mentions: []string{"12@s.whatsapp.net", "123@s.whatsapp.net"}}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	apply(t, s, whatsapp.PushName{JID: "12@s.whatsapp.net", Name: "Ann"})
	apply(t, s, whatsapp.PushName{JID: "123@s.whatsapp.net", Name: "Bob"})
	if m, _ := s.Message("g@g.us", "m1"); m.Text != "@Ann and @Bob" {
		t.Fatalf("got %q", m.Text)
	}
}

func TestMentionNamesAreNotScannedAgain(t *testing.T) {
	s, _ := newStore(t)
	jids := make([]string, 30)
	for i := range jids {
		jids[i] = "2@s.whatsapp.net"
	}
	if err := s.PutLive(whatsapp.NewMessage{Chat: "g@g.us", Kind: whatsapp.KindGroup, Message: whatsapp.Message{
		ID: "m1", Sender: "2@s.whatsapp.net", Time: 5, Text: "@2", Mentions: jids}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	apply(t, s, whatsapp.PushName{JID: "2@s.whatsapp.net", Name: "x@2x@2"})
	m, _ := s.Message("g@g.us", "m1")
	if m.Text != "@x@2x@2" || len(m.Mentions) != 1 {
		t.Fatalf("got %q %v", m.Text, m.Mentions)
	}
}

func TestManyStoredMentionsReadFast(t *testing.T) {
	s, _ := newStore(t)
	jids := make([]string, 5000)
	var text strings.Builder
	for i := range jids {
		jids[i] = fmt.Sprintf("%d@s.whatsapp.net", 1000+i)
		fmt.Fprintf(&text, "@%d ", 1000+i)
	}
	for text.Len() < 60000 {
		text.WriteString("filler @ text ")
	}
	if err := s.PutLive(whatsapp.NewMessage{Chat: "g@g.us", Kind: whatsapp.KindGroup, Message: whatsapp.Message{
		ID: "m1", Sender: "2@s.whatsapp.net", Time: 5, Text: text.String(), Mentions: jids}}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	m, err := s.Message("g@g.us", "m1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Messages("g@g.us", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > readBudget*time.Millisecond {
		t.Errorf("read took %v", d)
	}
	if len(m.Mentions) != whatsapp.MaxMentions || len(page.Messages[0].Mentions) != whatsapp.MaxMentions {
		t.Errorf("got %d mentions", len(m.Mentions))
	}
}

// A thumbnail above the cap is refused, whether it arrives with the Message or later. It is never cut.
func TestOversizedThumbnailIsRefused(t *testing.T) {
	s, _ := newStore(t)
	const jid = "a@s.whatsapp.net"
	big := make([]byte, whatsapp.MaxThumbnailBytes+1)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1, Messages: []whatsapp.Message{
		{ID: "m1", Time: 1, Media: &whatsapp.Media{Kind: media.Document, Key: make([]byte, 32), Thumbnail: big}},
		{ID: "m2", Time: 2, Media: &whatsapp.Media{Kind: media.Document, Key: make([]byte, 32), Thumbnail: big[:whatsapp.MaxThumbnailBytes]}},
	}}}})
	if r, err := s.MediaOf(jid, "m1"); err != nil || len(r.Thumbnail) != 0 {
		t.Errorf("inline thumbnail above the cap: len %d, %v; want dropped", len(r.Thumbnail), err)
	}
	if r, err := s.MediaOf(jid, "m2"); err != nil || len(r.Thumbnail) != whatsapp.MaxThumbnailBytes {
		t.Errorf("inline thumbnail at the cap: len %d, %v; want kept", len(r.Thumbnail), err)
	}
	if _, err := s.SetThumbnail(jid, "m1", big); !errors.Is(err, whatsapp.ErrThumbnailTooLarge) {
		t.Errorf("SetThumbnail above the cap = %v, want ErrThumbnailTooLarge", err)
	}
	if r, _ := s.MediaOf(jid, "m1"); len(r.Thumbnail) != 0 {
		t.Errorf("stored thumbnail has %d bytes, want none", len(r.Thumbnail))
	}
}

// The migration drops an oversized thumbnail, and its link, that an older Daemon stored.
func TestMigrationDropsAnOversizedStoredThumbnail(t *testing.T) {
	s, db := newStore(t)
	const jid = "a@s.whatsapp.net"
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: jid, LastActive: 1, Messages: []whatsapp.Message{
		{ID: "big", Time: 1, Media: &whatsapp.Media{Kind: media.Document, Key: make([]byte, 32), Thumbnail: []byte("a"), ThumbDirectPath: "/t"}},
		{ID: "small", Time: 2, Media: &whatsapp.Media{Kind: media.Document, Key: make([]byte, 32), Thumbnail: []byte("b")}},
	}}}})
	if _, err := db.Exec(`UPDATE media SET thumbnail = zeroblob(?) WHERE id = 'big'`, whatsapp.MaxThumbnailBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 22"); err != nil { // the version before the cap
		t.Fatal(err)
	}
	if err := chatstore.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.MediaOf(jid, "big"); len(r.Thumbnail) != 0 || r.ThumbDirectPath != "" {
		t.Errorf("oversized thumbnail kept: len %d, link %q", len(r.Thumbnail), r.ThumbDirectPath)
	}
	if r, _ := s.MediaOf(jid, "small"); string(r.Thumbnail) != "b" {
		t.Errorf("small thumbnail = %q, want kept", r.Thumbnail)
	}
}
