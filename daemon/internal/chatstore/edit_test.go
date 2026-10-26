package chatstore_test

import (
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const (
	editChat   = "2@s.whatsapp.net"
	editWindow = 15 * time.Minute
)

// An edit counts from the time of the edit itself, so a late delivery still applies and a forged late edit does not.
func TestEditAppliesInsideTheWindowAndUpdatesTheMentions(t *testing.T) {
	for _, tc := range []struct {
		name string
		time int64
		want bool
	}{
		{"at the end of the window", 1000 + int64(editWindow/time.Second), true},
		{"after the window", 1000 + int64(editWindow/time.Second) + 1, false},
	} {
		s, _ := newStore(t)
		m := whatsapp.Message{ID: "m1", Sender: editChat, Time: 1000, Text: "old @9", Mentions: []string{"9@s.whatsapp.net"}, MentionsMe: true}
		if _, err := s.PutLive(whatsapp.NewMessage{Chat: editChat, Message: m}, chatstore.LivePolicy{}); err != nil {
			t.Fatal(err)
		}
		ev := whatsapp.MessageEdited{Chat: editChat, ID: "m1", Sender: editChat, Text: "new", Time: tc.time}
		changed, err := s.Edit(ev, editWindow)
		if err != nil || changed != tc.want {
			t.Fatalf("%s: Edit = %v, %v; want %v", tc.name, changed, err, tc.want)
		}
		got, err := s.Message(editChat, "m1")
		if err != nil {
			t.Fatal(err)
		}
		wantText, wantMe := "old @9", true
		if tc.want {
			wantText, wantMe = "new", false // the new text has no mentions
		}
		if got.RawText != wantText || got.MentionsMe != wantMe || (len(got.Mentions) == 0) != tc.want {
			t.Errorf("%s: text %q, mentionsMe %v, mentions %v", tc.name, got.RawText, got.MentionsMe, got.Mentions)
		}
	}
}
