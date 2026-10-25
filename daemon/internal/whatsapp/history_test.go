package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// An older Message of the user comes in History Sync with its Delivery State. It must not fall back to sent.
func TestHistorySyncKeepsTheDeliveryStateOfOwnMessages(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop)}
	msg := func(id string, fromMe bool, status waWeb.WebMessageInfo_Status) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:     &waCommon.MessageKey{RemoteJID: proto.String("3161@s.whatsapp.net"), FromMe: proto.Bool(fromMe), ID: proto.String(id)},
			Message: &waE2E.Message{Conversation: proto.String("hi")},
			Status:  status.Enum(),
		}}
	}
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID: proto.String("3161@s.whatsapp.net"),
		Messages: []*waHistorySync.HistorySyncMsg{
			msg("read", true, waWeb.WebMessageInfo_READ),
			msg("delivered", true, waWeb.WebMessageInfo_DELIVERY_ACK),
			msg("played", true, waWeb.WebMessageInfo_PLAYED),
			msg("acked", true, waWeb.WebMessageInfo_SERVER_ACK),
			msg("theirs", false, waWeb.WebMessageInfo_READ),
		},
	}}}
	want := map[string]string{"read": StateRead, "delivered": StateDelivered, "played": StatePlayed, "acked": "", "theirs": ""}
	batch, _ := c.historySync(data)
	got := batch.Chats[0].Messages
	if len(got) != len(want) {
		t.Fatalf("got %d Messages, want %d", len(got), len(want))
	}
	for _, m := range got {
		if m.State != want[m.ID] {
			t.Errorf("Message %s: State = %q, want %q", m.ID, m.State, want[m.ID])
		}
	}
}
