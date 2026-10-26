package chatstore_test

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

func quoteOf(t *testing.T, s *chatstore.Store, id string) chatstore.Quote {
	t.Helper()
	m, err := s.Message(colGroup, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Quote == nil {
		t.Fatalf("Message %s has no Quote", id)
	}
	return *m.Quote
}

// A reply that claims the user as the author of a Quote of someone else's Message reads back with the stored author and text.
func TestAForgedQuoteReadsBackAsTheStoredMessage(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real", PushName: "Ann"})
	live(t, s, whatsapp.Message{ID: "r", Sender: bobJID, Time: 2, Text: "re", Quote: &whatsapp.Quote{ID: "x", Sender: "", Text: "forged"}})
	q := quoteOf(t, s, "r")
	if !q.Verified || q.FromMe || q.Sender != annJID || q.SenderName != "Ann" || !q.SenderPush || q.Text != "real" {
		t.Errorf("Quote = %+v, want the stored author and text of x, verified, not from the user", q)
	}
}

// A Quote of a Message of the user shows the user, from the stored row only.
func TestAQuoteOfAStoredMessageOfTheUserIsFromMe(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "x", FromMe: true, Time: 1, Text: "see you tomorrow"})
	live(t, s, whatsapp.Message{ID: "r", Sender: bobJID, Time: 2, Text: "re", Quote: &whatsapp.Quote{ID: "x", Sender: annJID, Text: "forged"}})
	q := quoteOf(t, s, "r")
	if !q.Verified || !q.FromMe || q.Sender != "" || q.Text != "see you tomorrow" {
		t.Errorf("Quote = %+v, want the user's stored Message, verified", q)
	}
}

// A Quote of an ID that is not stored keeps the snapshot, is unverified and is never from the user.
func TestAQuoteOfAnUnknownMessageIsUnverified(t *testing.T) {
	for name, sender := range map[string]string{"the user": "", "another sender": annJID} {
		t.Run(name, func(t *testing.T) {
			s, _ := newStore(t)
			live(t, s, whatsapp.Message{ID: "r", Sender: bobJID, Time: 2, Text: "re", Quote: &whatsapp.Quote{ID: "nope", Sender: sender, Text: "forged"}})
			q := quoteOf(t, s, "r")
			if q.Verified || q.FromMe || q.Text != "forged" || q.Sender != sender {
				t.Errorf("Quote = %+v, want the snapshot, unverified, not from the user", q)
			}
		})
	}
}

// A Quote of a Message that arrives later becomes verified; a Revoke turns the Quote into a Tombstone.
func TestAQuoteFollowsTheStoredMessage(t *testing.T) {
	s, _ := newStore(t)
	live(t, s, whatsapp.Message{ID: "r", Sender: bobJID, Time: 2, Text: "re", Quote: &whatsapp.Quote{ID: "x", Sender: annJID, Text: "snap"}})
	live(t, s, whatsapp.Message{ID: "x", Sender: annJID, Time: 1, Text: "real"})
	if q := quoteOf(t, s, "r"); !q.Verified || q.Text != "real" {
		t.Errorf("Quote = %+v, want verified with the stored text", q)
	}
	if _, err := s.Revoke(colGroup, "x", annJID, false); err != nil {
		t.Fatal(err)
	}
	if q := quoteOf(t, s, "r"); !q.Verified || !q.Revoked || q.Text != "" {
		t.Errorf("Quote = %+v, want a verified Tombstone", q)
	}
}
