package chatstore_test

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// The paired child of an HD photo or a motion photo replaces the Media of its parent. Only the author of the parent can do so.
// The caption, the Quote and the Reactions of the parent stay.
func TestReplaceMediaKeepsTheAuthorRuleAndTheRestOfTheMessage(t *testing.T) {
	const chat, author = "2@s.whatsapp.net", "2@s.whatsapp.net"
	for _, tc := range []struct {
		name   string
		sender string
		want   bool
	}{
		{"the author", author, true},
		{"another person", "3@s.whatsapp.net", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			var removed []string
			s.RemoveMedia = func(_, id string) { removed = append(removed, id) }
			parent := whatsapp.Message{ID: "p1", Sender: author, Time: 1000, Text: "sunset", Quote: &whatsapp.Quote{ID: "q1", Text: "where?"},
				Media: &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: 100, Key: []byte("sd"), DirectPath: "/sd", Thumbnail: []byte("thumb")}}
			if _, _, err := s.PutLive(whatsapp.NewMessage{Chat: chat, Message: parent}, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetReaction(chat, "p1", "3@s.whatsapp.net", false, "👍"); err != nil {
				t.Fatal(err)
			}
			hd := &whatsapp.Media{Kind: media.Video, Mime: "video/mp4", Size: 300, Key: []byte("hd"), DirectPath: "/hd", Duration: 2}
			changed, err := s.ReplaceMedia(whatsapp.MediaReplaced{Chat: chat, ID: "p1", Sender: tc.sender, Media: hd}, chatstore.MediaDownloading)
			if err != nil || changed != tc.want {
				t.Fatalf("ReplaceMedia = %v, %v; want %v", changed, err, tc.want)
			}
			ref, err := s.MediaOf(chat, "p1")
			if err != nil {
				t.Fatal(err)
			}
			wantPath, wantState, wantRemoved := "/sd", chatstore.MediaNone, 0
			if tc.want {
				wantPath, wantState, wantRemoved = "/hd", chatstore.MediaDownloading, 1 // the file of the old Media goes
			}
			if ref.DirectPath != wantPath || ref.State != wantState || len(removed) != wantRemoved {
				t.Errorf("Media at %q in state %q, %d files removed; want %q, %q, %d", ref.DirectPath, ref.State, len(removed), wantPath, wantState, wantRemoved)
			}
			if tc.want && (ref.Kind != media.Video || string(ref.Thumbnail) != "thumb") {
				t.Errorf("Media = %+v, want the video of the child with the thumbnail of the parent", ref.Media)
			}
			got, err := s.Message(chat, "p1")
			if err != nil {
				t.Fatal(err)
			}
			if tc.want && (got.Media == nil || got.Media.Duration != 2) {
				t.Errorf("Media = %+v, want the duration of the child", got.Media)
			}
			if got.Text != "sunset" || got.Quote == nil || got.Quote.ID != "q1" || len(got.Reactions) != 1 {
				t.Errorf("Message = %+v, want the caption, Quote and Reaction of the parent", got)
			}
		})
	}
}
