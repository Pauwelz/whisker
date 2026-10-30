package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// pnToLID is a LID store that knows the given phone to LID pairs only.
type pnToLID struct {
	store.LIDStore
	pairs map[types.JID]types.JID
}

func (s pnToLID) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	return s.pairs[pn], nil
}

// In a group that addresses by LID, the phone number of a quoted author or a reacted-to author must not go out.
func TestWireJIDHidesThePhoneInAGroupThatAddressesByLID(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	ownLID := types.JID{User: "9160", Server: types.HiddenUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	otherLID := types.JID{User: "9161", Server: types.HiddenUserServer}
	stranger := types.JID{User: "3162", Server: types.DefaultUserServer}
	lidGroup := types.JID{User: "1", Server: types.GroupServer}
	pnGroup := types.JID{User: "2", Server: types.GroupServer}
	unknownGroup := types.JID{User: "3", Server: types.GroupServer}
	dev := &store.Device{ID: &own, LID: ownLID, LIDs: pnToLID{pairs: map[types.JID]types.JID{other: otherLID}}}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop)}
	c.noteAddressing(lidGroup, types.AddressingModeLID)
	c.noteAddressing(pnGroup, types.AddressingModePN)
	c.noteAddressing(unknownGroup, "")

	for _, tc := range []struct {
		name       string
		chat, from types.JID
		want       types.JID
	}{
		{"author in a LID group", lidGroup, other, otherLID},
		{"the user in a LID group", lidGroup, own, ownLID},
		{"no mapping keeps the phone JID", lidGroup, stranger, stranger},
		{"PN group", pnGroup, other, other},
		{"group of unknown mode", unknownGroup, other, other},
		{"Direct Chat", other, other, other},
		{"empty sender (own Message)", lidGroup, types.EmptyJID, types.EmptyJID},
	} {
		if got := c.wireJID(tc.chat, tc.from); got != tc.want {
			t.Errorf("%s: wireJID = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := c.quoteSender(lidGroup, ""); got != ownLID.String() {
		t.Errorf("quoteSender of the user's own Message = %q, want %q", got, ownLID)
	}
	if got := c.quoteSender(lidGroup, other.String()); got != otherLID.String() {
		t.Errorf("quoteSender = %q, want %q", got, otherLID)
	}
}

func TestALiveGroupMessageTeachesTheAddressingMode(t *testing.T) {
	c := systemClient()
	evt := groupMessage("m1", &waE2E.Message{Conversation: proto.String("hi")})
	evt.Info.AddressingMode = types.AddressingModeLID
	c.handle(evt)
	if lid, _ := c.lidGroups.Load(sysGroup); lid != true {
		t.Errorf("lidGroups = %v, want true", lid)
	}
}

// A group with no event yet has an unknown mode. The first Quote or Reaction looks it up once, and the LID goes out.
func TestAnUnknownAddressingModeIsLookedUpOnce(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	otherLID := types.JID{User: "9161", Server: types.HiddenUserServer}
	group := types.JID{User: "1", Server: types.GroupServer}
	dev := &store.Device{ID: &own, LIDs: pnToLID{pairs: map[types.JID]types.JID{other: otherLID}}}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop)}
	calls := 0
	c.groupInfo = func(context.Context, types.JID) (*types.GroupInfo, error) {
		calls++
		return &types.GroupInfo{AddressingMode: types.AddressingModeLID}, nil
	}
	for i := 0; i < 2; i++ {
		c.learnAddressing(group)
		if got := c.wireJID(group, other); got != otherLID {
			t.Errorf("wireJID = %v, want %v", got, otherLID)
		}
	}
	if calls != 1 {
		t.Errorf("lookups = %d, want 1", calls)
	}
}

// A failed lookup keeps the phone JID and is tried again at the next send.
func TestAFailedAddressingLookupKeepsThePhoneJID(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	group := types.JID{User: "1", Server: types.GroupServer}
	c := &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own}, waLog.Noop)}
	calls := 0
	c.groupInfo = func(context.Context, types.JID) (*types.GroupInfo, error) {
		calls++
		return nil, errors.New("offline")
	}
	c.learnAddressing(group)
	c.learnAddressing(group)
	if got := c.wireJID(group, other); got != other {
		t.Errorf("wireJID = %v, want %v", got, other)
	}
	if calls != 2 {
		t.Errorf("lookups = %d, want 2", calls)
	}
}

// A media retry receipt names the author by LID in a LID group, and not at all in a Direct Chat.
func TestRetryInfoNamesTheLIDSenderInALIDGroup(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	otherLID := types.JID{User: "9161", Server: types.HiddenUserServer}
	group := types.JID{User: "1", Server: types.GroupServer}
	dev := &store.Device{ID: &own, LIDs: pnToLID{pairs: map[types.JID]types.JID{other: otherLID}}}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop)}
	c.noteAddressing(group, types.AddressingModeLID)

	if got := c.retryInfo(group, other.String(), "m1", false); got.Sender != otherLID || !got.IsGroup || got.ID != "m1" {
		t.Errorf("group info = %+v, want sender %v", got, otherLID)
	}
	if got := c.retryInfo(other, other.String(), "m1", false); !got.Sender.IsEmpty() || got.IsGroup {
		t.Errorf("direct info = %+v, want no sender", got)
	}
}

// A Direct Chat with a known LID gets the receipt by LID. Without a LID the phone JID goes out.
func TestRetryInfoAddressesADirectChatByLID(t *testing.T) {
	own := types.JID{User: "3160", Server: types.DefaultUserServer}
	other := types.JID{User: "3161", Server: types.DefaultUserServer}
	otherLID := types.JID{User: "9161", Server: types.HiddenUserServer}
	stranger := types.JID{User: "3162", Server: types.DefaultUserServer}
	dev := &store.Device{ID: &own, LIDs: pnToLID{pairs: map[types.JID]types.JID{other: otherLID}}}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop)}

	if got := c.retryInfo(other, "", "m1", false); got.Chat != otherLID {
		t.Errorf("chat = %v, want %v", got.Chat, otherLID)
	}
	if got := c.retryInfo(stranger, "", "m1", false); got.Chat != stranger {
		t.Errorf("chat = %v, want %v", got.Chat, stranger)
	}
}

// Two Chats can hold the same Message ID. The answer for one must not use the media key of the other.
func TestMediaRetryAnswerIsKeyedByChat(t *testing.T) {
	c := systemClient()
	chatA := types.JID{User: "3161", Server: types.DefaultUserServer}
	chatB := types.JID{User: "3162", Server: types.DefaultUserServer}
	c.retries.Store(retryKey{chatA.String(), "m1"}, []byte("a"))
	c.retries.Store(retryKey{chatB.String(), "m1"}, []byte("b"))

	c.handle(&events.MediaRetry{MessageID: "m1", ChatID: chatB, Error: &events.MediaRetryError{Code: 2}})
	if got := (<-c.events).(MediaRetried); got.Chat != chatB.String() || !got.NotOnPhone {
		t.Errorf("event = %+v, want not on phone in %v", got, chatB)
	}
	if _, kept := c.retries.Load(retryKey{chatA.String(), "m1"}); !kept {
		t.Error("the request of the other Chat was dropped")
	}
	c.ForgetMediaRetry(chatA.String(), "m1")
	c.handle(&events.MediaRetry{MessageID: "m1", ChatID: chatA, Error: &events.MediaRetryError{Code: 2}})
	select {
	case ev := <-c.events:
		t.Errorf("event %+v after the request was forgotten", ev)
	default:
	}
}
