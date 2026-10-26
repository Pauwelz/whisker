package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
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

// A revoke names its sender: the store applies it only when the sender is the author.
func TestRevokeCarriesTheSender(t *testing.T) {
	c := systemClient()
	evt := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: sysGroup, Sender: jef, IsGroup: true}, Timestamp: sysAt},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("m1")},
		}},
	}
	if !c.handleProtocol(evt) {
		t.Fatal("handleProtocol = false; want the revoke consumed")
	}
	want := MessageRevoked{Chat: sysGroup.String(), ID: "m1", Sender: jef.String(), FromMe: false}
	if got := <-c.events; got != want {
		t.Errorf("emitted %+v, want %+v", got, want)
	}
}

// An admin revoke carries the admin in the stanza and the author in the key. A plain revoke with the same
// attribute outside a group, or a key from the user, stays an author revoke.
func TestAdminRevokeNamesTheAuthorAndTheAdmin(t *testing.T) {
	revoke := func(edit types.EditAttribute, group, keyFromMe bool) MessageRevoked {
		c := systemClient()
		evt := &events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: sysGroup, Sender: jef, IsGroup: group}, Timestamp: sysAt, Edit: edit},
			Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
				Key:  &waCommon.MessageKey{ID: proto.String("m1"), FromMe: proto.Bool(keyFromMe), Participant: proto.String("3202@s.whatsapp.net")},
			}},
		}
		if !c.handleProtocol(evt) {
			t.Fatal("handleProtocol = false; want the revoke consumed")
		}
		return (<-c.events).(MessageRevoked)
	}
	want := MessageRevoked{Chat: sysGroup.String(), ID: "m1", Sender: "3202@s.whatsapp.net", Admin: jef.String()}
	if got := revoke(types.EditAttributeAdminRevoke, true, false); got != want {
		t.Errorf("admin revoke = %+v, want %+v", got, want)
	}
	author := MessageRevoked{Chat: sysGroup.String(), ID: "m1", Sender: jef.String()}
	for name, got := range map[string]MessageRevoked{
		"sender revoke":   revoke(types.EditAttributeSenderRevoke, true, false),
		"no group":        revoke(types.EditAttributeAdminRevoke, false, false),
		"key from the me": revoke(types.EditAttributeAdminRevoke, true, true),
	} {
		if got != author {
			t.Errorf("%s = %+v, want %+v", name, got, author)
		}
	}
}
