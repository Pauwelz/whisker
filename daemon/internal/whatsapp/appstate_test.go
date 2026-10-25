package whatsapp

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" database/sql driver
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// The first App State sync after a link is a full sync. whatsmeow drops its events (Pin, Mute, Read Marker) unless
// asked, so the pins of the phone never reached the chat store.
func TestNewClientGetsTheEventsOfAFullAppStateSync(t *testing.T) {
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:"+filepath.Join(t.TempDir(), "device.db")+"?_pragma=foreign_keys(1)", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(container)
	if err != nil {
		t.Fatal(err)
	}
	if !c.cli.EmitAppStateEventsOnFullSync {
		t.Error("NewClient leaves EmitAppStateEventsOnFullSync off: a full App State sync sends no Pin events")
	}
}

// The flag events of a full sync wait for its end and go out as one AppStateBatch. A later change goes out alone.
func TestAFullSyncSendsItsFlagsAsOneBatch(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop), events: make(chan Event, 8)}
	chat := types.JID{User: "3161", Server: types.DefaultUserServer}
	c.handle(&events.Pin{JID: chat, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}, FromFullSync: true})
	c.handle(&events.MarkChatAsRead{JID: chat, Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(false)}, FromFullSync: true})
	if len(c.events) != 0 {
		t.Fatalf("a full-sync event went out before the sync ended: %T", <-c.events)
	}
	c.handle(&events.AppStateSyncComplete{})
	want := AppStateBatch{Events: []Event{Pin{JID: "3161@s.whatsapp.net", Pinned: true}, ReadMarker{JID: "3161@s.whatsapp.net", Unread: true}}}
	if got := <-c.events; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	c.handle(&events.Pin{JID: chat, Action: &waSyncAction.PinAction{Pinned: proto.Bool(false)}})
	if got := <-c.events; got != (Pin{JID: "3161@s.whatsapp.net"}) {
		t.Errorf("a live Pin = %+v, want it alone", got)
	}
}

func TestArchiveAndUnarchiveSettingEventsMap(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop), events: make(chan Event, 8)}
	chat := types.JID{User: "3161", Server: types.DefaultUserServer}
	c.handle(&events.Archive{JID: chat, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}})
	if got := <-c.events; got != (Archive{JID: "3161@s.whatsapp.net", Archived: true}) {
		t.Errorf("Archive = %+v", got)
	}
	c.handle(&events.UnarchiveChatsSetting{Action: &waSyncAction.UnarchiveChatsSetting{UnarchiveChats: proto.Bool(true)}})
	if got := <-c.events; got != (UnarchiveChats{On: true}) {
		t.Errorf("UnarchiveChats = %+v", got)
	}
}
