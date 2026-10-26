package whatsapp

import (
	"context"
	"testing"

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
		Info: types.MessageInfo{ID: "m2", MessageSource: types.MessageSource{Chat: other, Sender: other}},
		Message: &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType:    waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
			TargetMessageKey: &waCommon.MessageKey{RemoteJID: proto.String(other.String()), FromMe: proto.Bool(false), ID: proto.String("m1")},
			EncPayload:       payload, EncIV: iv,
		}},
	})

	if len(c.events) != 1 {
		t.Fatalf("got %d events, want one MessageEdited", len(c.events))
	}
	want := MessageEdited{Chat: other.String(), ID: "m1", Sender: other.String(), Text: "fixed typo"}
	if got := <-c.events; got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
