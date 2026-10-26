package chatstore_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	colGroup = "g@g.us"
	annJID   = "1@s.whatsapp.net"
	bobJID   = "2@s.whatsapp.net"
)

func live(t *testing.T, s *chatstore.Store, m whatsapp.Message) bool {
	t.Helper()
	isNew, err := s.PutLive(whatsapp.NewMessage{Chat: colGroup, Kind: whatsapp.KindGroup, Message: m}, chatstore.LivePolicy{MediaState: chatstore.MediaDownloading, MarkUnread: true})
	if err != nil {
		t.Fatal(err)
	}
	return isNew
}

func pic(thumb string) *whatsapp.Media {
	return &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Key: make([]byte, 32), Thumbnail: []byte(thumb), ThumbDirectPath: thumb}
}

// A Participant who reuses the ID of a stored Message changes nothing: the text, the author and the Media stay.
func TestAReusedIDOfAnotherAuthorChangesNothing(t *testing.T) {
	for name, reuse := range map[string]whatsapp.Message{
		"another sender": {ID: "x", Sender: bobJID, Time: 2, Text: "fake", Media: pic("t")},
		"the user":       {ID: "x", FromMe: true, Time: 2, Text: "fake", Media: pic("t")},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newStore(t)
			if !live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real"}) {
				t.Fatal("the first Message is not new")
			}
			if live(t, s, reuse) {
				t.Error("the reused ID counts as a new Message")
			}
			m, err := s.Message(colGroup, "x")
			if err != nil {
				t.Fatal(err)
			}
			if m.Text != "real" || m.Sender != annJID || m.FromMe || m.Media != nil {
				t.Errorf("Message = %+v, want the first one unchanged", m)
			}
			if _, err := s.MediaOf(colGroup, "x"); !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("MediaOf error = %v, want no Media row", err)
			}
		})
	}
}

// The thumbnail fill is for a copy of the same author only.
func TestAReusedIDOfAnotherAuthorFillsNoThumbnail(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Media: pic("")})
	live(t, s, whatsapp.Message{ID: "x", Sender: bobJID, Time: 2, Media: pic("t")})
	r, err := s.MediaOf(colGroup, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Thumbnail) != 0 || r.ThumbDirectPath != "" {
		t.Errorf("Media = %+v, want no thumbnail of the other author", r.Media)
	}
}

// A live copy of the same Message is not new, and it still fills a missing thumbnail.
func TestARedeliveryIsNotNew(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Media: pic("")})
	if live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Media: pic("t")}) {
		t.Error("a redelivery counts as new")
	}
	if r, err := s.MediaOf(colGroup, "x"); err != nil || string(r.Thumbnail) != "t" {
		t.Errorf("Media = %+v, %v, want the thumbnail of the redelivery", r.Media, err)
	}
}

// The stored row wins over a History Sync row of another author.
func TestHistorySyncDoesNotReplaceAStoredMessageOfAnotherAuthor(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "y", Sender: bobJID, Time: 1, Text: "bobJID"})
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: colGroup, Kind: whatsapp.KindGroup, LastActive: 1, Messages: []whatsapp.Message{
		{ID: "y", Sender: annJID, Time: 1, Text: "annJID", Media: pic("t")},
	}}}})
	m, err := s.Message(colGroup, "y")
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "bobJID" || m.Sender != bobJID || m.Media != nil {
		t.Errorf("Message = %+v, want the stored one unchanged", m)
	}
}
