package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func withTimer() *waE2E.ContextInfo { return &waE2E.ContextInfo{Expiration: proto.Uint32(86400)} }

// Every kind that contentOf shows, as a Message that carries a timer.
func timedKinds() map[string]*waE2E.Message {
	return map[string]*waE2E.Message{
		"text":          {ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi"), ContextInfo: withTimer()}},
		"image":         {ImageMessage: &waE2E.ImageMessage{ContextInfo: withTimer()}},
		"video":         {VideoMessage: &waE2E.VideoMessage{ContextInfo: withTimer()}},
		"audio":         {AudioMessage: &waE2E.AudioMessage{ContextInfo: withTimer()}},
		"sticker":       {StickerMessage: &waE2E.StickerMessage{ContextInfo: withTimer()}},
		"document":      {DocumentMessage: &waE2E.DocumentMessage{ContextInfo: withTimer()}},
		"location":      {LocationMessage: &waE2E.LocationMessage{ContextInfo: withTimer()}},
		"live location": {LiveLocationMessage: &waE2E.LiveLocationMessage{ContextInfo: withTimer()}},
		"contact":       {ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ann"), ContextInfo: withTimer()}},
		"contacts":      {ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: []*waE2E.ContactMessage{{DisplayName: proto.String("Ann")}}, ContextInfo: withTimer()}},
		"poll":          {PollCreationMessage: &waE2E.PollCreationMessage{ContextInfo: withTimer()}},
		"poll v2":       {PollCreationMessageV2: &waE2E.PollCreationMessage{ContextInfo: withTimer()}},
		"poll v3":       {PollCreationMessageV3: &waE2E.PollCreationMessage{ContextInfo: withTimer()}},
		"poll v4":       {PollCreationMessageV4: &waE2E.FutureProofMessage{Message: &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{ContextInfo: withTimer()}}}},
		"poll v5":       {PollCreationMessageV5: &waE2E.PollCreationMessage{ContextInfo: withTimer()}},
		"poll v6":       {PollCreationMessageV6: &waE2E.PollCreationMessage{ContextInfo: withTimer()}},
		"event":         {EventMessage: &waE2E.EventMessage{ContextInfo: withTimer()}},
		"unsupported":   {GroupInviteMessage: &waE2E.GroupInviteMessage{ContextInfo: withTimer()}},
	}
}

func TestTimerOfEveryShownKind(t *testing.T) {
	for name, m := range timedKinds() {
		t.Run(name, func(t *testing.T) {
			if _, ok := contentOf(m, false); !ok {
				t.Fatal("contentOf does not show this kind")
			}
			if got := timerOf(m); got != 86400 {
				t.Errorf("timerOf = %d, want 86400", got)
			}
		})
	}
}

// Any content field of waE2E.Message that has a contextInfo gives its timer, so the list cannot drift from the proto.
func TestTimerOfEveryContentFieldOfTheProto(t *testing.T) {
	fields := (&waE2E.Message{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() || fd.Name() == "messageContextInfo" {
			continue
		}
		m := &waE2E.Message{}
		mr := m.ProtoReflect()
		content := mr.NewField(fd).Message()
		ci := content.Descriptor().Fields().ByName("contextInfo")
		if ci == nil {
			continue
		}
		content.Set(ci, protoreflect.ValueOfMessage(withTimer().ProtoReflect()))
		mr.Set(fd, protoreflect.ValueOfMessage(content))
		if got := timerOf(m); got != 86400 {
			t.Errorf("%s: timerOf = %d, want 86400", fd.Name(), got)
		}
	}
}

func TestTimerOfIgnoresTheMessageContextInfo(t *testing.T) {
	m := &waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}, LocationMessage: &waE2E.LocationMessage{}}
	if got := timerOf(m); got != 0 {
		t.Errorf("timerOf = %d, want 0", got)
	}
	if got := timerOf(&waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}}); got != 0 {
		t.Errorf("timerOf = %d, want 0", got)
	}
}
