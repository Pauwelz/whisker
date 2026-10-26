package whatsapp

import (
	"fmt"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestContentOf(t *testing.T) {
	vcard := "BEGIN:VCARD\nFN:Ann\nTEL;type=CELL;waid=3247:+32 47 000 00 00\nEND:VCARD"
	tests := []struct {
		name     string
		msg      *waE2E.Message
		viewOnce bool
		want     string // "" with ok false means hidden
		ok       bool
	}{
		{"text", &waE2E.Message{Conversation: proto.String("hi")}, false, "hi", true},
		{"poll", &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Lunch?")}}, false, "Poll: Lunch?. Open it on your phone.", true},
		{"event", &waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Party")}}, false, "Event: Party. Open it on your phone.", true},
		{"view once flag", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true)}}, false, viewOnceText, true},
		{"view once wrapper", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, true, viewOnceText, true},
		{"contact", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ann"), Vcard: proto.String(vcard)}}, false, "Contact: Ann, +32 47 000 00 00", true},
		{"contact without number", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ann")}}, false, "Contact: Ann", true},
		{"contacts", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: []*waE2E.ContactMessage{
			{DisplayName: proto.String("Ann")}, {DisplayName: proto.String("Bob")}}}}, false, "Contact: Ann\nContact: Bob", true},
		{"location", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{Name: proto.String("Cafe"), DegreesLatitude: proto.Float64(51.5), DegreesLongitude: proto.Float64(-0.12)}}, false,
			"Location: Cafe\nhttps://www.openstreetmap.org/?mlat=51.500000&mlon=-0.120000#map=17/51.500000/-0.120000", true},
		{"unknown kind", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{}}, false, unsupportedText, true},
		{"unknown kind with metadata", &waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{}, MessageContextInfo: &waE2E.MessageContextInfo{}}, false, unsupportedText, true},
		{"reaction", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{}}, false, "", false},
		{"poll vote", &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{}}, false, "", false},
		{"encrypted edit", &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{}}, false, "", false},
		{"metadata only", &waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}}, false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := contentOf(tt.msg, tt.viewOnce)
			if ok != tt.ok || got.Text != tt.want {
				t.Errorf("got %q, %v; want %q, %v", got.Text, ok, tt.want, tt.ok)
			}
			if strings.Contains(tt.name, "view once") && got.Media != nil {
				t.Error("view-once Media must not be kept")
			}
		})
	}
}

func TestMentions(t *testing.T) {
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("@111 hi"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"111@s.whatsapp.net"}},
	}}
	got, _ := contentOf(msg, false)
	if len(got.Mentions) != 1 || got.Mentions[0] != "111@s.whatsapp.net" {
		t.Fatalf("mentions: %v", got.Mentions)
	}
	if !mentionsUser(got.Mentions, "999", "111") || mentionsUser(got.Mentions, "999", "") {
		t.Error("mentionsUser compares the user part, not an empty user")
	}
	if !mentionsUser([]string{"111:5@lid"}, "111") {
		t.Error("a device suffix is ignored")
	}
}

func TestMediaOfKeepsTheDurationOfAudioAndVideo(t *testing.T) {
	voice := mediaOf(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Seconds: proto.Uint32(12)}})
	video := mediaOf(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(95)}})
	if voice.Duration != 12 || video.Duration != 95 {
		t.Errorf("durations = %d, %d; want 12, 95", voice.Duration, video.Duration)
	}
}

func TestMediaOfMarksAVideoThatPlaysAsAGif(t *testing.T) {
	gif := mediaOf(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true)}})
	video := mediaOf(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{}})
	if !gif.Gif || video.Gif {
		t.Errorf("gif = %v, video = %v; want true, false", gif.Gif, video.Gif)
	}
}

func TestMediaOfKeepsTheLinkToTheSeparateThumbnail(t *testing.T) {
	im := mediaOf(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{ThumbnailDirectPath: proto.String("/t"), ThumbnailSHA256: []byte{1}, ThumbnailEncSHA256: []byte{2}}})
	vm := mediaOf(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{ThumbnailDirectPath: proto.String("/v")}})
	dm := mediaOf(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{ThumbnailDirectPath: proto.String("/d")}})
	if im.ThumbDirectPath != "/t" || im.ThumbSHA256[0] != 1 || im.ThumbEncSHA256[0] != 2 || vm.ThumbDirectPath != "/v" || dm.ThumbDirectPath != "/d" {
		t.Errorf("thumbnail links = %+v, %+v, %+v", im, vm, dm)
	}
}

func TestMentionsInDropsRepeatsAndTokensNotInText(t *testing.T) {
	jids := []string{"12@s.whatsapp.net", "123@s.whatsapp.net", "123@s.whatsapp.net", "9@s.whatsapp.net"}
	got := MentionsIn("hi @123, bye", jids)
	if len(got) != 1 || got[0] != "123@s.whatsapp.net" {
		t.Fatalf("got %v", got)
	}
	many := make([]string, 0, 1000)
	text := ""
	for i := 0; i < 1000; i++ {
		many = append(many, fmt.Sprintf("%d@s.whatsapp.net", i))
		text += fmt.Sprintf("@%d ", i)
	}
	if got := MentionsIn(text, many); len(got) != MaxMentions {
		t.Fatalf("got %d mentions, want %d", len(got), MaxMentions)
	}
}
