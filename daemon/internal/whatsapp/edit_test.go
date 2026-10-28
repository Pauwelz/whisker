package whatsapp

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// oneSecret is a message secret store that holds the secret of one Message.
type oneSecret struct {
	store.MsgSecretStore
	sender types.JID
	secret []byte
}

func (s oneSecret) GetMessageSecret(_ context.Context, _, _ types.JID, _ types.MessageID) ([]byte, types.JID, error) {
	return s.secret, s.sender, nil
}

// WhatsApp sends the edit of another person as a SecretEncryptedMessage. Undecrypted, it showed as "Unsupported message".
func TestAnEncryptedEditOfAnotherPersonBecomesMessageEdited(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	secret := make([]byte, 32)
	dev := &store.Device{ID: &own, MsgSecrets: oneSecret{sender: other, secret: secret}}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop), events: make(chan Event, 8)}

	// Encrypt like WhatsApp does: the key derives from the target id, its author, the editor and the use case.
	plain, _ := proto.Marshal(&waE2E.Message{Conversation: proto.String("fixed typo")})
	key := hkdfutil.SHA256(secret, nil, []byte("m1"+other.String()+other.String()+string(whatsmeow.EncSecretMessageEdit)), 32)
	iv := make([]byte, 12)
	payload, err := gcmutil.Encrypt(key, iv, plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.handle(&events.Message{
		Info: types.MessageInfo{ID: "m2", MessageSource: types.MessageSource{Chat: other, Sender: other}, Timestamp: time.Unix(5000, 0)},
		Message: &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType:    waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
			TargetMessageKey: &waCommon.MessageKey{RemoteJID: proto.String(other.String()), FromMe: proto.Bool(false), ID: proto.String("m1")},
			EncPayload:       payload, EncIV: iv,
		}},
	})

	if len(c.events) != 1 {
		t.Fatalf("got %d events, want one MessageEdited", len(c.events))
	}
	want := MessageEdited{Chat: other.String(), ID: "m1", Sender: other.String(), Text: "fixed typo", Time: 5000}
	if got := <-c.events; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// An edit carries its own time and the mentions of its new text, so the store can check the window and update the highlight.
func TestAnEditCarriesItsTimeAndMentions(t *testing.T) {
	c := systemClient()
	edit := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("hi @3160"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{sysOwn.String(), "9@s.whatsapp.net"}},
	}}
	evt := groupMessage("e1", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waCommon.MessageKey{ID: proto.String("m1")}, EditedMessage: edit,
	}})
	if !c.handleProtocol(evt) {
		t.Fatal("handleProtocol = false")
	}
	got := (<-c.events).(MessageEdited)
	if got.Time != sysAt.Unix() || !got.MentionsMe || !reflect.DeepEqual(got.Mentions, []string{sysOwn.String()}) {
		t.Errorf("got %+v, want Time %d, MentionsMe and only the mention that the text holds", got, sysAt.Unix())
	}
}

// A LID group mentions by LID. The text then names the phone JID, as the mention list does, so the store can label it.
func TestALIDMentionNamesThePhoneJIDInTheText(t *testing.T) {
	text := func(s string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(s), ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{sysOwnL.String(), "9@s.whatsapp.net"}},
		}}
	}
	want := []string{sysOwn.String(), "9@s.whatsapp.net"}

	c := systemClient()
	c.handle(groupMessage("m1", text("@77 Sup @9")))
	got := (<-c.events).(NewMessage).Message
	if got.Text != "@3160 Sup @9" || !reflect.DeepEqual(got.Mentions, want) || !got.MentionsMe {
		t.Errorf("live: got text %q, mentions %v, mentionsMe %v", got.Text, got.Mentions, got.MentionsMe)
	}

	evt := groupMessage("e1", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waCommon.MessageKey{ID: proto.String("m1")}, EditedMessage: text("@77 again @9"),
	}})
	c.handleProtocol(evt)
	edit := (<-c.events).(MessageEdited)
	if edit.Text != "@3160 again @9" || !reflect.DeepEqual(edit.Mentions, want) {
		t.Errorf("edit: got text %q, mentions %v", edit.Text, edit.Mentions)
	}
}
