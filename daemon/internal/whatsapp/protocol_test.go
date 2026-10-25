package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// An ordinary text has no ProtocolMessage. Its zero type is REVOKE, so a missing nil check
// dropped every incoming Message as a revoke.
func TestHandleProtocolIgnoresOrdinaryMessages(t *testing.T) {
	c := &Client{events: make(chan Event, 1)}
	evt := &events.Message{Message: &waE2E.Message{Conversation: proto.String("hi")}}

	if c.handleProtocol(evt) {
		t.Fatal("handleProtocol took an ordinary text as a protocol Message")
	}
	if len(c.events) != 0 {
		t.Fatalf("handleProtocol emitted %T for an ordinary text", <-c.events)
	}
}

// whatsmeow decodes the mute end of App State as milliseconds. The port holds Unix seconds.
func TestAppStateMuteSeconds(t *testing.T) {
	for _, tc := range []struct{ ms, want int64 }{{1_900_000_000_000, 1_900_000_000}, {-1, -1}, {0, 0}} {
		if got := appStateMuteSeconds(tc.ms); got != tc.want {
			t.Errorf("appStateMuteSeconds(%d) = %d, want %d", tc.ms, got, tc.want)
		}
	}
}

func TestDeleteForMePatchIndexEndsWithTheAuthor(t *testing.T) {
	chat := types.JID{User: "1", Server: types.DefaultUserServer}
	group := types.JID{User: "12-34", Server: types.GroupServer}
	for _, tc := range []struct {
		name         string
		chat         types.JID
		sender       string
		fromMe       bool
		wantOwn, who string
	}{
		{"own Message", chat, "", true, "1", "0"},
		{"Message of the other person in a Direct Chat", chat, "1@s.whatsapp.net", false, "0", "0"},
		{"Message of a Group Participant", group, "5@s.whatsapp.net", false, "0", "5@s.whatsapp.net"},
	} {
		idx := deleteForMePatch(tc.chat, "m1", tc.sender, tc.fromMe, 7).Mutations[0].Index
		if idx[0] != "deleteMessageForMe" || idx[2] != "m1" || idx[3] != tc.wantOwn || idx[4] != tc.who {
			t.Errorf("%s: index = %v", tc.name, idx)
		}
	}
}
