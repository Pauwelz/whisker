package contract_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const crew = "g1@g.us"

var (
	ann = whatsapp.Member{Phone: "1@s.whatsapp.net", LID: "11@lid"}
	bob = whatsapp.Member{Phone: "2@s.whatsapp.net", LID: "22@lid"}
)

// groupRunner is a connected Daemon with the Group Chat crew, a Direct Chat, and a History Sync Message "h1" of the user in crew.
func groupRunner(t *testing.T, members ...whatsapp.Member) *runner {
	t.Helper()
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetGroup(crew, members...)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: crew, Kind: whatsapp.KindGroup, LastActive: 1, Messages: []whatsapp.Message{{ID: "h1", FromMe: true, Time: 1, State: whatsapp.StateSent}}},
		{JID: "9@s.whatsapp.net", Kind: whatsapp.KindDirect, LastActive: 1},
	}})
	r.nextFrame() // chatsChanged
	r.port.Emit(whatsapp.Connected{})
	r.nextFrame() // connected
	return r
}

func (r *runner) sendTo(chat string) {
	r.send(`{"jsonrpc":"2.0","id":1,"method":"chat.send","params":{"chat":"` + chat + `","text":"hi"}}`)
	r.nextFrame() // the pending Message
	if f := r.nextFrame(); !strings.Contains(f, `"state":"sent"`) {
		r.t.Fatalf("frame = %s, want sent", f)
	}
}

func (r *runner) expectFrame(want string) {
	r.t.Helper()
	if f := r.nextFrame(); !strings.Contains(f, want) {
		r.t.Fatalf("frame = %s, want %s", f, want)
	}
}

func (r *runner) expectQuiet() {
	r.t.Helper()
	select {
	case f := <-r.frames:
		r.t.Fatalf("unexpected frame %s", f)
	case <-time.After(150 * time.Millisecond):
	}
}

func (r *runner) waitLookups(n int) {
	r.t.Helper()
	for i := 0; r.port.GroupLookups() < n; i++ {
		if i > 2000 {
			r.t.Fatalf("group lookups = %d, want %d", r.port.GroupLookups(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func receiptFrom(sender, state string) whatsapp.Receipt {
	return whatsapp.Receipt{Chat: crew, IDs: []string{"fake-1"}, State: state, Sender: sender}
}

func TestGroupSendMovesOnlyAfterTheLastRecipient(t *testing.T) {
	r := groupRunner(t, ann, bob)
	r.sendTo(crew)
	r.waitLookups(1)
	r.port.Emit(receiptFrom(ann.Phone, whatsapp.StateRead))
	r.expectQuiet()
	r.port.Emit(receiptFrom(bob.LID, whatsapp.StateDelivered)) // Bob by LID, Ann by phone JID
	r.expectFrame(`"state":"delivered"`)
	r.port.Emit(receiptFrom(bob.Phone, whatsapp.StateRead))
	r.expectFrame(`"state":"read"`)
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
}

func TestGroupLookupFailureIsRetriedOnTheNextReceipt(t *testing.T) {
	r := groupRunner(t, ann)
	r.port.SetGroupError(errors.New("lookup failed"))
	r.sendTo(crew)
	r.waitLookups(1)
	r.waitLog("group lookup failed")
	r.port.SetGroupError(nil)
	r.port.Emit(receiptFrom(ann.Phone, whatsapp.StateDelivered))
	r.expectFrame(`"state":"delivered"`)
	if got := r.port.GroupLookups(); got != 2 {
		t.Errorf("group lookups = %d, want 2", got)
	}
}

func fromThePhone() whatsapp.NewMessage {
	return whatsapp.NewMessage{Chat: crew, Kind: whatsapp.KindGroup, Message: whatsapp.Message{ID: "fake-1", FromMe: true, Time: 2, Text: "from the phone"}}
}

func TestMessageFromAnotherDeviceMovesOnlyAfterTheLastRecipient(t *testing.T) {
	r := groupRunner(t, ann, bob)
	r.port.Emit(fromThePhone())
	r.waitLookups(1)
	r.port.Emit(receiptFrom(ann.LID, whatsapp.StateRead))
	r.port.Emit(receiptFrom(bob.Phone, whatsapp.StateDelivered))
	for {
		if f := r.nextFrame(); strings.Contains(f, `"state":"delivered"`) {
			break
		}
	}
	r.port.Emit(receiptFrom(bob.LID, whatsapp.StateRead))
	r.expectFrame(`"state":"read"`)
}

func TestReceiptBeforeTheMemberLookupEndsStillCounts(t *testing.T) {
	r := groupRunner(t, ann)
	r.port.HoldGroupLookup(true)
	r.port.Emit(fromThePhone())
	r.waitLookups(1)
	for { // the arrival frames
		if f := r.nextFrame(); strings.Contains(f, "chatsChanged") {
			break
		}
	}
	r.port.Emit(receiptFrom(ann.LID, whatsapp.StateRead))
	r.expectQuiet() // no set yet, so nothing moves
	r.port.HoldGroupLookup(false)
	for {
		if f := r.nextFrame(); strings.Contains(f, `"state":"read"`) {
			break
		}
	}
}

func TestHistorySyncMessageOfAGroupIgnoresReceipts(t *testing.T) {
	r := groupRunner(t, ann)
	r.port.Emit(whatsapp.Receipt{Chat: crew, IDs: []string{"h1"}, State: whatsapp.StateRead, Sender: ann.Phone})
	r.expectQuiet()
	if got := r.port.GroupLookups(); got != 0 {
		t.Errorf("group lookups = %d, want none: a History Sync Message gets no set", got)
	}
}

func TestDirectChatReceiptIsUnchanged(t *testing.T) {
	r := groupRunner(t)
	r.sendTo("9@s.whatsapp.net")
	r.port.Emit(whatsapp.Receipt{Chat: "9@s.whatsapp.net", IDs: []string{"fake-1"}, State: whatsapp.StateRead})
	r.expectFrame(`"state":"read"`)
	if got := r.port.GroupLookups(); got != 0 {
		t.Errorf("group lookups = %d, want none in a Direct Chat", got)
	}
}
