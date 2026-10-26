package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// A delete from another device of the user reaches the Daemon as one event, live and in a full sync, under the phone JID.
func TestRemoteDeletesMapToOneEventUnderThePhoneJID(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	ownLID := types.JID{User: "777", Server: types.HiddenUserServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own, LID: ownLID}, waLog.Noop), events: make(chan Event, 8)}
	phone := types.JID{User: "3161", Server: types.DefaultUserServer}
	clear := &waSyncAction.ClearChatAction{MessageRange: &waSyncAction.SyncActionMessageRange{LastMessageTimestamp: proto.Int64(55)}}
	tests := []struct {
		name string
		evt  any
		want Event
	}{
		{"delete for me", &events.DeleteForMe{ChatJID: phone, MessageID: "m1"}, DeletedForMe{Chat: "3161@s.whatsapp.net", ID: "m1"}},
		{"delete for me from a full sync", &events.DeleteForMe{ChatJID: phone, MessageID: "m1", FromFullSync: true}, DeletedForMe{Chat: "3161@s.whatsapp.net", ID: "m1"}},
		{"delete for me in a LID Chat", &events.DeleteForMe{ChatJID: ownLID, MessageID: "m1"}, DeletedForMe{Chat: "3160@s.whatsapp.net", ID: "m1"}},
		{"clear chat", &events.ClearChat{JID: phone, Action: clear}, ChatCleared{Chat: "3161@s.whatsapp.net", Until: 55}},
		{"clear chat from a full sync", &events.ClearChat{JID: phone, Action: clear, FromFullSync: true}, ChatCleared{Chat: "3161@s.whatsapp.net", Until: 55}},
		{"clear chat without a range", &events.ClearChat{JID: phone, Action: &waSyncAction.ClearChatAction{}}, ChatCleared{Chat: "3161@s.whatsapp.net"}},
		{"clear chat in a LID Chat", &events.ClearChat{JID: ownLID, Action: clear}, ChatCleared{Chat: "3160@s.whatsapp.net", Until: 55}},
		{"delete chat", &events.DeleteChat{JID: phone}, ChatDeleted{Chat: "3161@s.whatsapp.net"}},
		{"delete chat from a full sync", &events.DeleteChat{JID: phone, FromFullSync: true}, ChatDeleted{Chat: "3161@s.whatsapp.net"}},
		{"delete chat in a LID Chat", &events.DeleteChat{JID: ownLID}, ChatDeleted{Chat: "3160@s.whatsapp.net"}},
	}
	for _, tc := range tests {
		c.handle(tc.evt)
		if len(c.events) != 1 {
			t.Fatalf("%s: %d events, want 1", tc.name, len(c.events))
		}
		if got := <-c.events; got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
