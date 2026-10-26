package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const reservedID = "sys-0123456789abcdef"

func groupMessage(id string, m *waE2E.Message) *events.Message {
	return &events.Message{
		Info:    types.MessageInfo{ID: id, MessageSource: types.MessageSource{Chat: sysGroup, Sender: jef, IsGroup: true}, Timestamp: sysAt},
		Message: m,
	}
}

func reactionTo(target, emoji string) *waE2E.Message {
	return &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String(target)}, Text: proto.String(emoji)}}
}

// A Participant can pre-send a Message under the predictable ID of a System Message, and so hide it.
func TestALiveMessageWithAReservedIDIsDropped(t *testing.T) {
	c := systemClient()
	c.handle(groupMessage(reservedID, &waE2E.Message{Conversation: proto.String("hi")}))
	c.handle(groupMessage("m1", &waE2E.Message{Conversation: proto.String("hi")}))
	if len(c.events) != 1 {
		t.Fatalf("got %d events, want only the Message with an ordinary ID", len(c.events))
	}
	if got := (<-c.events).(NewMessage).Message.ID; got != "m1" {
		t.Errorf("emitted Message %q, want m1", got)
	}
}

func TestAQuoteOfAReservedIDIsDropped(t *testing.T) {
	c := systemClient()
	quote := func(id string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("re"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String(id)}}}
	}
	if q := c.quoteOf(quote(reservedID)); q != nil {
		t.Errorf("quote of a reserved ID = %+v, want none", q)
	}
	if q := c.quoteOf(quote("m1")); q == nil || q.ID != "m1" {
		t.Errorf("quote of m1 = %+v", q)
	}
}

func TestEditsAndRevokesOfAReservedIDAreDropped(t *testing.T) {
	c := systemClient()
	for _, typ := range []waE2E.ProtocolMessage_Type{waE2E.ProtocolMessage_REVOKE, waE2E.ProtocolMessage_MESSAGE_EDIT} {
		evt := groupMessage("p1", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: typ.Enum(), Key: &waCommon.MessageKey{ID: proto.String(reservedID)},
			EditedMessage: &waE2E.Message{Conversation: proto.String("new")},
		}})
		if !c.handleProtocol(evt) {
			t.Errorf("%v: handleProtocol = false, want the Message consumed", typ)
		}
	}
	if len(c.events) != 0 {
		t.Fatalf("emitted %T for a reserved target", <-c.events)
	}
}

func TestIncomingReactionsAreChecked(t *testing.T) {
	for _, tc := range []struct {
		name, target, emoji string
		want                bool
	}{
		{"default emoji", "m1", "👍", true},
		{"removal", "m1", "", true},
		{"free text", "m1", "VERIFIED by the bank", false},
		{"emoji outside the picker", "m1", "🔥", true},
		{"emoji with a skin tone", "m1", "👍🏽", true},
		{"ZWJ family", "m1", "👨‍👩‍👧‍👦", true},
		{"flag", "m1", "🇳🇱", true},
		{"keycap", "m1", "1️⃣", true},
		{"text with an emoji", "m1", "ok👍", false},
		{"digit without keycap", "m1", "1", false},
		{"too long", "m1", "👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍👍", false},
		{"reserved target", reservedID, "👍", false},
	} {
		c := systemClient()
		c.handle(groupMessage("r1", reactionTo(tc.target, tc.emoji)))
		if got := len(c.events) == 1; got != tc.want {
			t.Errorf("%s: emitted = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHistorySyncDropsReservedIDsAndBadReactions(t *testing.T) {
	c := systemClient()
	msg := func(id string, reactions ...*waWeb.Reaction) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:       &waCommon.MessageKey{RemoteJID: proto.String("3201@s.whatsapp.net"), ID: proto.String(id)},
			Message:   &waE2E.Message{Conversation: proto.String("hi")},
			Reactions: reactions,
		}}
	}
	react := func(text string) *waWeb.Reaction {
		return &waWeb.Reaction{Key: &waCommon.MessageKey{FromMe: proto.Bool(true)}, Text: proto.String(text)}
	}
	batch, _ := c.historySync(&waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID:       proto.String("3201@s.whatsapp.net"),
		Messages: []*waHistorySync.HistorySyncMsg{msg(reservedID), msg("m1", react("👍"), react("🔥"), react("fake label"), react(""))},
	}}})
	got := batch.Chats[0].Messages
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("Messages = %+v, want only m1", got)
	}
	if len(got[0].Reactions) != 2 || got[0].Reactions[0].Emoji != "👍" || got[0].Reactions[1].Emoji != "🔥" {
		t.Errorf("Reactions = %+v, want 👍 and 🔥", got[0].Reactions)
	}
}
