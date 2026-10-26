package chatstore_test

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const group = "g1@g.us"

var (
	ann = whatsapp.Participant{Phone: "1@s.whatsapp.net", LID: "11@lid"}
	bob = whatsapp.Participant{Phone: "2@s.whatsapp.net", LID: "22@lid"}
)

// groupMessage stores a pending Message of the user in a Group Chat.
func groupMessage(t *testing.T) *chatstore.Store {
	t.Helper()
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: group, Kind: whatsapp.KindGroup, LastActive: 1}}})
	if _, err := s.AddPending(group, "m1", "hi", nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func receipt(t *testing.T, s *chatstore.Store, sender, state string) {
	t.Helper()
	if tracked, err := s.RecordReceipt(group, "m1", sender, state); err != nil || !tracked {
		t.Fatalf("RecordReceipt = %v, %v; want tracked", tracked, err)
	}
}

func combined(t *testing.T, s *chatstore.Store) (string, bool) {
	t.Helper()
	state, known, err := s.Combined(group, "m1")
	if err != nil {
		t.Fatal(err)
	}
	return state, known
}

func TestCombinedIsTheLowestStateOverAllRecipients(t *testing.T) {
	s := groupMessage(t)
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann, bob}); err != nil {
		t.Fatal(err)
	}
	if state, known := combined(t, s); state != "" || !known {
		t.Fatalf("no receipt: state %q known %v, want empty and known", state, known)
	}
	receipt(t, s, ann.Phone, whatsapp.StatePlayed)
	if state, _ := combined(t, s); state != "" {
		t.Errorf("one of two Recipients: state = %q, want empty", state)
	}
	receipt(t, s, bob.Phone, whatsapp.StateDelivered)
	if state, _ := combined(t, s); state != whatsapp.StateDelivered {
		t.Errorf("played and delivered: state = %q, want delivered", state)
	}
	receipt(t, s, bob.Phone, whatsapp.StateRead)
	if state, _ := combined(t, s); state != whatsapp.StateRead {
		t.Errorf("played and read: state = %q, want read", state)
	}
}

func TestCombinedOfAnEmptySetNeverMoves(t *testing.T) {
	s := groupMessage(t)
	if err := s.SetRecipients(group, "m1", nil); err != nil {
		t.Fatal(err)
	}
	receipt(t, s, ann.Phone, whatsapp.StateRead)
	if state, known := combined(t, s); state != "" || !known {
		t.Errorf("empty set: state = %q, known = %v; want empty and known", state, known)
	}
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann}); err != nil {
		t.Fatal(err)
	}
	if state, _ := combined(t, s); state != "" {
		t.Errorf("a later set changed the empty set: state = %q", state)
	}
}

func TestReceiptBeforeTheSetCountsOnceTheSetIsThere(t *testing.T) {
	s := groupMessage(t)
	receipt(t, s, ann.LID, whatsapp.StateRead)
	if _, known := combined(t, s); known {
		t.Fatal("known before SetRecipients")
	}
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann}); err != nil {
		t.Fatal(err)
	}
	if state, _ := combined(t, s); state != whatsapp.StateRead {
		t.Errorf("state = %q, want read", state)
	}
}

func TestTwoDevicesOfOneRecipientCountOnce(t *testing.T) {
	s := groupMessage(t)
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann, bob}); err != nil {
		t.Fatal(err)
	}
	receipt(t, s, ann.Phone, whatsapp.StateDelivered) // the phone of Ann
	receipt(t, s, ann.LID, whatsapp.StateDelivered)   // her laptop, by LID
	if state, _ := combined(t, s); state != "" {
		t.Errorf("Ann twice, Bob never: state = %q, want empty", state)
	}
}

func TestARecipientKeepsItsHighestState(t *testing.T) {
	s := groupMessage(t)
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann}); err != nil {
		t.Fatal(err)
	}
	receipt(t, s, ann.Phone, whatsapp.StateRead)
	receipt(t, s, ann.Phone, whatsapp.StateDelivered) // a late, lower receipt
	if state, _ := combined(t, s); state != whatsapp.StateRead {
		t.Errorf("state = %q, want read", state)
	}
}

func TestTheSetIsFrozenOnce(t *testing.T) {
	s := groupMessage(t)
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann, bob}); err != nil {
		t.Fatal(err)
	}
	receipt(t, s, ann.Phone, whatsapp.StateRead)
	if state, _ := combined(t, s); state != whatsapp.StateRead {
		t.Errorf("state = %q, want read: Bob joined after the send", state)
	}
}

func TestAHistorySyncMessageIsNotTracked(t *testing.T) {
	s, _ := newStore(t)
	apply(t, s, whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: group, Kind: whatsapp.KindGroup, LastActive: 1,
		Messages: []whatsapp.Message{{ID: "m1", FromMe: true, Time: 1, State: whatsapp.StateDelivered}}}}})
	if err := s.SetRecipients(group, "m1", []whatsapp.Participant{ann}); err != nil {
		t.Fatal(err)
	}
	if tracked, err := s.RecordReceipt(group, "m1", ann.Phone, whatsapp.StateRead); err != nil || tracked {
		t.Errorf("RecordReceipt = %v, %v; want not tracked", tracked, err)
	}
	if _, known := combined(t, s); known {
		t.Error("a History Sync Message has a set")
	}
}

func TestALiveMessageFromAnotherDeviceIsTracked(t *testing.T) {
	s, _ := newStore(t)
	live := whatsapp.NewMessage{Chat: group, Kind: whatsapp.KindGroup, Message: whatsapp.Message{ID: "m1", FromMe: true, Time: 1}}
	if _, err := s.PutLive(live, chatstore.LivePolicy{}); err != nil {
		t.Fatal(err)
	}
	if tracked, err := s.RecordReceipt(group, "m1", ann.Phone, whatsapp.StateRead); err != nil || !tracked {
		t.Errorf("RecordReceipt = %v, %v; want tracked", tracked, err)
	}
}
