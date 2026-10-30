package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// fakeInner records the calls that must reach whatsmeow's own buffer. Its outgoing methods must stay unused.
type fakeInner struct {
	store.EventBuffer // nil: an outgoing call panics
	calls             []string
}

func (f *fakeInner) GetBufferedEvent(context.Context, [32]byte) (*store.BufferedEvent, error) {
	f.calls = append(f.calls, "get")
	return nil, nil
}
func (f *fakeInner) PutBufferedEvent(context.Context, [32]byte, []byte, time.Time) error {
	f.calls = append(f.calls, "put")
	return nil
}
func (f *fakeInner) DoDecryptionTxn(context.Context, func(context.Context) error) error {
	f.calls = append(f.calls, "txn")
	return nil
}
func (f *fakeInner) ClearBufferedEventPlaintext(context.Context, [32]byte) error {
	f.calls = append(f.calls, "clear")
	return nil
}
func (f *fakeInner) DeleteOldBufferedHashes(context.Context) error {
	f.calls = append(f.calls, "old")
	return nil
}

type retryRow struct {
	chat, id, target, format string
	data                     []byte
}

type fakeRetries struct {
	added   []retryRow
	cleaned int
	gotChat string
	gotAlt  string
	gotID   string
	err     error
}

func (f *fakeRetries) AddRetry(chat, id, target, format string, data []byte) error {
	f.added = append(f.added, retryRow{chat, id, target, format, data})
	return nil
}
func (f *fakeRetries) Retry(chat, alt, id string) (string, []byte, error) {
	f.gotChat, f.gotAlt, f.gotID = chat, alt, id
	return "wa", []byte("x"), f.err
}
func (f *fakeRetries) DeleteOldRetries() error { f.cleaned++; return nil }

func TestRetryBufferPassesTheIncomingMethodsToTheInnerBuffer(t *testing.T) {
	inner := &fakeInner{}
	b := newRetryBuffer(inner, &fakeRetries{})
	ctx := context.Background()
	b.GetBufferedEvent(ctx, [32]byte{})
	b.PutBufferedEvent(ctx, [32]byte{}, nil, time.Time{})
	b.DoDecryptionTxn(ctx, nil)
	b.ClearBufferedEventPlaintext(ctx, [32]byte{})
	b.DeleteOldBufferedHashes(ctx)
	want := []string{"get", "put", "txn", "clear", "old"}
	if len(inner.calls) != len(want) {
		t.Fatalf("inner calls = %v; want %v", inner.calls, want)
	}
	for i := range want {
		if inner.calls[i] != want[i] {
			t.Errorf("inner calls = %v; want %v", inner.calls, want)
		}
	}
}

func TestRetryBufferKeepsOutgoingEventsInTheRetryStore(t *testing.T) {
	r := &fakeRetries{}
	b := newRetryBuffer(&fakeInner{}, r)
	ctx := context.Background()
	chat := types.NewJID("5", types.HiddenUserServer)
	if err := b.AddOutgoingEvent(ctx, chat, "id1", "wa", []byte{0xff}); err == nil {
		t.Error("an invalid proto must fail") // the wa format must parse, so a bad row never hides a target
	}
	if err := b.DeleteOldOutgoingEvents(ctx); err != nil || r.cleaned != 1 {
		t.Errorf("DeleteOldOutgoingEvents = %v, cleaned %d; want nil, 1", err, r.cleaned)
	}
	alt := types.NewJID("6", types.DefaultUserServer)
	format, data, err := b.GetOutgoingEvent(ctx, chat, alt, "id1")
	if err != nil || format != "wa" || string(data) != "x" {
		t.Errorf("GetOutgoingEvent = %q, %q, %v", format, data, err)
	}
	if r.gotChat != chat.String() || r.gotAlt != alt.String() || r.gotID != "id1" {
		t.Errorf("Retry called with %q %q %q", r.gotChat, r.gotAlt, r.gotID)
	}
}

func TestRetryBufferGivesTheNotFoundResultOfWhatsmeow(t *testing.T) {
	r := &fakeRetries{err: sql.ErrNoRows}
	_, _, err := newRetryBuffer(&fakeInner{}, r).GetOutgoingEvent(context.Background(), types.JID{}, types.JID{}, "id")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v; want sql.ErrNoRows", err)
	}
	if r.gotAlt != "" {
		t.Errorf("alt = %q; an empty JID must give an empty string", r.gotAlt)
	}
}

func TestRetryBufferStoresTheTargetOfAnEditRevokeOrReaction(t *testing.T) {
	key := &waCommon.MessageKey{ID: proto.String("target")}
	for name, tc := range map[string]struct {
		msg    *waE2E.Message
		target string
	}{
		"text":     {&waE2E.Message{Conversation: proto.String("hi")}, ""},
		"edit":     {&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Key: key, Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum()}}, "target"},
		"revoke":   {&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Key: key, Type: waE2E.ProtocolMessage_REVOKE.Enum()}}, ""},
		"reaction": {&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key, Text: proto.String("+")}}, "target"},
		"no key":   {&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := proto.Marshal(tc.msg)
			if err != nil {
				t.Fatal(err)
			}
			r := &fakeRetries{}
			if err := newRetryBuffer(&fakeInner{}, r).AddOutgoingEvent(context.Background(), types.NewJID("5", types.HiddenUserServer), "id", "wa", data); err != nil {
				t.Fatal(err)
			}
			if len(r.added) != 1 || r.added[0].target != tc.target || r.added[0].id != "id" || r.added[0].format != "wa" || r.added[0].chat != "5@lid" {
				t.Errorf("added = %+v; want one row with target %q", r.added, tc.target)
			}
		})
	}
}

func TestRetryBufferStoresAnFbMessageWithoutATarget(t *testing.T) {
	r := &fakeRetries{}
	if err := newRetryBuffer(&fakeInner{}, r).AddOutgoingEvent(context.Background(), types.JID{}, "id", "fb", []byte("not parsed")); err != nil {
		t.Fatal(err)
	}
	if len(r.added) != 1 || r.added[0].target != "" {
		t.Errorf("added = %+v; want one row without a target", r.added)
	}
}
