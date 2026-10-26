package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func receiptClient() *Client {
	me := types.JID{User: "1", Server: types.DefaultUserServer}
	lid := types.JID{User: "11", Server: types.HiddenUserServer}
	dev := &store.Device{ID: &me, LID: lid}
	return &Client{cli: whatsmeow.NewClient(dev, nil), events: make(chan Event, 4)}
}

func groupReceipt(sender types.JID) *events.Receipt {
	return &events.Receipt{
		MessageSource: types.MessageSource{Chat: types.JID{User: "g1", Server: types.GroupServer}, Sender: sender, IsGroup: true},
		MessageIDs:    []types.MessageID{"m1"},
		Type:          types.ReceiptTypeRead,
	}
}

func TestGroupDeliveredFromAnOwnDeviceIsDropped(t *testing.T) {
	c := receiptClient()
	for _, sender := range []types.JID{{User: "1", Device: 3, Server: types.DefaultUserServer}, {User: "11", Device: 3, Server: types.HiddenUserServer}} {
		r := groupReceipt(sender)
		r.Type = types.ReceiptTypeDelivered
		c.handle(r)
	}
	if len(c.events) != 0 {
		t.Fatalf("emitted %T for a receipt of an own device", <-c.events)
	}
}

func TestGroupReceiptSenderLosesItsDevicePart(t *testing.T) {
	c := receiptClient()
	c.handle(groupReceipt(types.JID{User: "2", Device: 5, Server: types.DefaultUserServer}))
	r, ok := (<-c.events).(Receipt)
	if !ok || r.Sender != "2@s.whatsapp.net" || r.State != StateRead {
		t.Fatalf("receipt = %+v, want sender 2@s.whatsapp.net, state read", r)
	}
}

func TestReadOnAnOwnDeviceIsReadElsewhere(t *testing.T) {
	own := types.JID{User: "1", Device: 3, Server: types.DefaultUserServer}
	direct := types.MessageSource{Chat: types.JID{User: "2", Server: types.DefaultUserServer}, Sender: own, IsFromMe: true}
	group := types.MessageSource{Chat: types.JID{User: "g1", Server: types.GroupServer}, Sender: own, IsFromMe: true, IsGroup: true}
	for _, tc := range []struct {
		src  types.MessageSource
		typ  types.ReceiptType
		chat string
	}{
		{direct, types.ReceiptTypeRead, "2@s.whatsapp.net"},
		{direct, types.ReceiptTypeReadSelf, "2@s.whatsapp.net"},
		{group, types.ReceiptTypeRead, "g1@g.us"},
		{group, types.ReceiptTypeReadSelf, "g1@g.us"},
	} {
		c := receiptClient()
		c.handle(&events.Receipt{MessageSource: tc.src, MessageIDs: []types.MessageID{"in1"}, Type: tc.typ})
		if len(c.events) == 0 {
			t.Errorf("%s in %s: nothing emitted", tc.typ, tc.chat)
			continue
		}
		if got, ok := (<-c.events).(ReadElsewhere); !ok || got.Chat != tc.chat || len(got.IDs) != 1 || got.IDs[0] != "in1" {
			t.Errorf("%s in %s: emitted %+v, want ReadElsewhere of in1", tc.typ, tc.chat, got)
		}
	}
}
