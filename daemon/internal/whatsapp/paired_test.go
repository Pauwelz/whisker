package whatsapp

import (
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

// pairedChild is the second Message of a photo sent in HD or as a motion photo. It names the parent "p1".
func pairedChild(paired waE2E.ContextInfo_PairedMediaType, assoc waE2E.MessageAssociation_AssociationType, inner *waE2E.Message) *waE2E.Message {
	return &waE2E.Message{
		AssociatedChildMessage: &waE2E.FutureProofMessage{Message: inner},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageAssociation: &waE2E.MessageAssociation{
			AssociationType:  assoc.Enum(),
			ParentMessageKey: &waCommon.MessageKey{ID: proto.String("p1")},
		}},
	}
}

func hdImageChild() *waE2E.Message {
	return pairedChild(waE2E.ContextInfo_HD_IMAGE_CHILD, waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(900000), MediaKey: []byte("hd-key"), DirectPath: proto.String("/hd"),
		ContextInfo: &waE2E.ContextInfo{PairedMediaType: waE2E.ContextInfo_HD_IMAGE_CHILD.Enum()},
	}})
}

func motionPhotoChild() *waE2E.Message {
	return pairedChild(waE2E.ContextInfo_MOTION_PHOTO_CHILD, waE2E.MessageAssociation_MOTION_PHOTO, &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Mimetype: proto.String("video/mp4"), FileLength: proto.Uint64(300000), MediaKey: []byte("mp-key"), DirectPath: proto.String("/mp"), Seconds: proto.Uint32(2),
		ContextInfo: &waE2E.ContextInfo{PairedMediaType: waE2E.ContextInfo_MOTION_PHOTO_CHILD.Enum()},
	}})
}

// WhatsApp sends an HD photo or a motion photo as a parent and a child. The child replaces the Media of the parent.
func TestAPairedChildReplacesTheMediaOfItsParent(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	for _, tc := range []struct {
		name  string
		child *waE2E.Message
		want  Media
	}{
		{"HD image", hdImageChild(), Media{Kind: media.Image, Mime: "image/jpeg", Size: 900000, Key: []byte("hd-key"), DirectPath: "/hd"}},
		{"motion photo", motionPhotoChild(), Media{Kind: media.Video, Mime: "video/mp4", Size: 300000, Key: []byte("mp-key"), DirectPath: "/mp", Duration: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop), events: make(chan Event, 8)}
			c.handle(&events.Message{
				Info:    types.MessageInfo{ID: "c1", MessageSource: types.MessageSource{Chat: other, Sender: other}, Timestamp: time.Unix(5001, 0)},
				Message: tc.child,
			})
			if len(c.events) != 1 {
				t.Fatalf("got %d events, want one MediaReplaced", len(c.events))
			}
			got, ok := (<-c.events).(MediaReplaced)
			if !ok {
				t.Fatalf("event is not MediaReplaced")
			}
			if got.Chat != other.String() || got.ID != "p1" || got.Sender != other.String() || got.FromMe {
				t.Errorf("got %+v, want the parent p1 of %s", got, other)
			}
			if got.Media == nil || !reflect.DeepEqual(*got.Media, tc.want) {
				t.Errorf("Media = %+v, want %+v", got.Media, tc.want)
			}
			if got.Child.Message.ID != "c1" || got.Child.Message.Media == nil {
				t.Errorf("Child = %+v, want Message c1 with the Media, to store when the parent is missing", got.Child)
			}
		})
	}
}

// History Sync often lists the child before the parent. Either way the batch holds one Message: the parent with the Media of the child.
// A child whose parent is not in the batch stays as a Message of its own.
func TestHistorySyncPairsAChildWithItsParent(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop)}
	msg := func(id string, m *waE2E.Message) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:     &waCommon.MessageKey{RemoteJID: proto.String("3161@s.whatsapp.net"), FromMe: proto.Bool(false), ID: proto.String(id)},
			Message: m,
		}}
	}
	parent := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), Caption: proto.String("sunset"), DirectPath: proto.String("/sd")}}
	for _, tc := range []struct {
		name string
		msgs []*waHistorySync.HistorySyncMsg
		want map[string]string // Message ID to the DirectPath of its Media
	}{
		{"child first", []*waHistorySync.HistorySyncMsg{msg("c1", hdImageChild()), msg("p1", parent)}, map[string]string{"p1": "/hd"}},
		{"parent first", []*waHistorySync.HistorySyncMsg{msg("p1", parent), msg("c1", motionPhotoChild())}, map[string]string{"p1": "/mp"}},
		{"no parent", []*waHistorySync.HistorySyncMsg{msg("c1", hdImageChild())}, map[string]string{"c1": "/hd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{ID: proto.String("3161@s.whatsapp.net"), Messages: tc.msgs}}}
			batch, _ := c.historySync(data)
			got := map[string]string{}
			for _, m := range batch.Chats[0].Messages {
				if m.Media == nil {
					t.Fatalf("Message %s has no Media", m.ID)
				}
				got[m.ID] = m.Media.DirectPath
				if m.ID == "p1" && m.Text != "sunset" {
					t.Errorf("parent Text = %q, want the caption of the parent", m.Text)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
