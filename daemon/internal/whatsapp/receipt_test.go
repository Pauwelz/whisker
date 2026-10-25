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

func TestGroupReceiptFromAnOwnDeviceIsDropped(t *testing.T) {
	c := receiptClient()
	c.handle(groupReceipt(types.JID{User: "1", Device: 3, Server: types.DefaultUserServer}))
	c.handle(groupReceipt(types.JID{User: "11", Device: 3, Server: types.HiddenUserServer}))
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
