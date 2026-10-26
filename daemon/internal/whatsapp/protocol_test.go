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

// An admin revoke of the user's own Message names the user as the author. A LID admin keeps its LID as the other ID.
// The user's own admin revoke from the phone is an admin revoke too.
func TestAdminRevokeOwnMessageLIDAdminAndOwnAdmin(t *testing.T) {
	revoke := func(src types.MessageSource, author types.JID) MessageRevoked {
		c := systemClient()
		src.Chat, src.IsGroup = sysGroup, true
		evt := &events.Message{
			Info: types.MessageInfo{MessageSource: src, Timestamp: sysAt, Edit: types.EditAttributeAdminRevoke},
			Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
				Key:  &waCommon.MessageKey{ID: proto.String("m1"), Participant: proto.String(author.String())},
			}},
		}
		c.handleProtocol(evt)
		return (<-c.events).(MessageRevoked)
	}
	chat := sysGroup.String()
	if got, want := revoke(types.MessageSource{Sender: jef}, sysOwn), (MessageRevoked{Chat: chat, ID: "m1", FromMe: true, Admin: jef.String()}); got != want {
		t.Errorf("own Message = %+v, want %+v", got, want)
	}
	if got, want := revoke(types.MessageSource{Sender: jefLID, SenderAlt: jef}, rob), (MessageRevoked{Chat: chat, ID: "m1", Sender: rob.String(), Admin: jef.String(), AdminAlt: jefLID.String()}); got != want {
		t.Errorf("LID admin = %+v, want %+v", got, want)
	}
	if got, want := revoke(types.MessageSource{Sender: sysOwn, IsFromMe: true}, rob), (MessageRevoked{Chat: chat, ID: "m1", Sender: rob.String(), Admin: sysOwn.String()}); got != want {
		t.Errorf("own admin = %+v, want %+v", got, want)
	}
}
