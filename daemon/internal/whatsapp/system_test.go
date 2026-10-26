package whatsapp

import (
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	sysGroup = types.JID{User: "120363", Server: types.GroupServer}
	sysOwn   = types.JID{User: "3160", Server: types.DefaultUserServer}
	sysOwnL  = types.JID{User: "77", Server: types.HiddenUserServer}
	jef      = types.JID{User: "3201", Server: types.DefaultUserServer}
	jefLID   = types.JID{User: "55", Server: types.HiddenUserServer}
	rob      = types.JID{User: "3202", Server: types.DefaultUserServer}
	ann      = types.JID{User: "3203", Server: types.DefaultUserServer}
	piet     = types.JID{User: "3204", Server: types.DefaultUserServer}
	sysAt    = time.Unix(1_700_000_000, 0)
)

func systemClient() *Client {
	own := sysOwn
	return &Client{cli: whatsmeow.NewClient(&store.Device{ID: &own, LID: sysOwnL}, waLog.Noop), events: make(chan Event, 16)}
}

// sysMsg is what the port must emit, without the ID.
type sysMsg struct {
	Sender string
	FromMe bool
	System System
}

// liveSystem runs one live event and returns the System Messages it emitted, and whether a ChatTimer came too.
func liveSystem(t *testing.T, evt any) (msgs []Message, timer bool) {
	t.Helper()
	c := systemClient()
	c.handle(evt)
	close(c.events)
	for ev := range c.events {
		switch ev := ev.(type) {
		case NewMessage:
			if ev.Message.System == nil {
				t.Fatalf("emitted a Message that is not a System Message: %+v", ev.Message)
			}
			msgs = append(msgs, ev.Message)
		case ChatTimer:
			timer = true
		}
	}
	return msgs, timer
}

func shape(msgs []Message) []sysMsg {
	var out []sysMsg
	for _, m := range msgs {
		out = append(out, sysMsg{m.Sender, m.FromMe, *m.System})
	}
	return out
}

func TestLiveChangesBecomeSystemMessages(t *testing.T) {
	info := func(i events.GroupInfo) *events.GroupInfo {
		i.JID, i.Timestamp = sysGroup, sysAt
		if i.Sender == nil {
			i.Sender = &jef
		}
		return &i
	}
	for _, tc := range []struct {
		name string
		evt  any
		want []sysMsg
	}{
		{"add", info(events.GroupInfo{Join: []types.JID{rob, ann}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemAdd, Targets: []string{rob.String(), ann.String()}}}}},
		{"join by link", &events.GroupInfo{JID: sysGroup, Timestamp: sysAt, JoinReason: "invite", Join: []types.JID{rob}},
			[]sysMsg{{"", false, System{Kind: SystemInvite, Targets: []string{rob.String()}}}}},
		{"leave", info(events.GroupInfo{Sender: &piet, Leave: []types.JID{piet}}),
			[]sysMsg{{piet.String(), false, System{Kind: SystemLeave, Targets: []string{piet.String()}}}}},
		{"remove", info(events.GroupInfo{Leave: []types.JID{piet}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemRemove, Targets: []string{piet.String()}}}}},
		{"promote you", info(events.GroupInfo{Promote: []types.JID{sysOwnL}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemPromote, Targets: []string{""}}}}},
		{"demote", info(events.GroupInfo{Demote: []types.JID{ann}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemDemote, Targets: []string{ann.String()}}}}},
		{"subject by you", info(events.GroupInfo{Sender: &sysOwn, Name: &types.GroupName{Name: "Family"}}),
			[]sysMsg{{sysOwn.String(), true, System{Kind: SystemSubject, Value: "Family"}}}},
		{"description", info(events.GroupInfo{Topic: &types.GroupTopic{Topic: "x"}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemDescription}}}},
		{"description deleted", info(events.GroupInfo{Topic: &types.GroupTopic{TopicDeleted: true}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemDescription, Value: SystemDeleted}}}},
		{"icon removed", &events.Picture{JID: sysGroup, Author: jef, Timestamp: sysAt, Remove: true},
			[]sysMsg{{jef.String(), false, System{Kind: SystemIcon, Value: SystemDeleted}}}},
		{"icon of a person is no System Message", &events.Picture{JID: ann, Author: ann, Timestamp: sysAt}, nil},
		{"create", &events.JoinedGroup{Type: "new", Sender: &jef, GroupInfo: types.GroupInfo{JID: sysGroup, GroupCreated: sysAt}},
			[]sysMsg{{jef.String(), false, System{Kind: SystemCreate}}}},
		{"added you", &events.JoinedGroup{Sender: &jef, GroupInfo: types.GroupInfo{JID: sysGroup}},
			[]sysMsg{{jef.String(), false, System{Kind: SystemAdd, Targets: []string{""}}}}},
		{"you joined by link", &events.JoinedGroup{Reason: "invite", GroupInfo: types.GroupInfo{JID: sysGroup}},
			[]sysMsg{{"", false, System{Kind: SystemInvite, Targets: []string{""}}}}},
		{"LID actor takes the phone JID", info(events.GroupInfo{Sender: &jefLID, SenderPN: &jef, Name: &types.GroupName{Name: "F"}}),
			[]sysMsg{{jef.String(), false, System{Kind: SystemSubject, Value: "F"}}}},
		{"one System Message per kind", info(events.GroupInfo{Join: []types.JID{rob, ann}, Leave: []types.JID{jef}, Name: &types.GroupName{Name: "Family"}}),
			[]sysMsg{
				{jef.String(), false, System{Kind: SystemAdd, Targets: []string{rob.String(), ann.String()}}},
				{jef.String(), false, System{Kind: SystemLeave, Targets: []string{jef.String()}}},
				{jef.String(), false, System{Kind: SystemSubject, Value: "Family"}},
			}},
		{"other changes are dropped", info(events.GroupInfo{Announce: &types.GroupAnnounce{IsAnnounce: true}}), nil},
	} {
		msgs, _ := liveSystem(t, tc.evt)
		if got := shape(msgs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
		for _, m := range msgs {
			if m.Time != sysAt.Unix() && tc.name != "added you" && tc.name != "you joined by link" {
				t.Errorf("%s: Time = %d, want %d", tc.name, m.Time, sysAt.Unix())
			}
		}
	}
}

func TestLiveTimerChangeIsASystemMessageInEveryChat(t *testing.T) {
	group := &events.GroupInfo{JID: sysGroup, Sender: &jef, Timestamp: sysAt, Ephemeral: &types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 604800}}
	direct := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: piet, Sender: piet}, Timestamp: sysAt},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(), EphemeralExpiration: proto.Uint32(0),
		}},
	}
	for name, tc := range map[string]struct {
		evt  any
		want sysMsg
	}{
		"group":  {group, sysMsg{jef.String(), false, System{Kind: SystemTimer, Value: "604800"}}},
		"direct": {direct, sysMsg{piet.String(), false, System{Kind: SystemTimer, Value: "0"}}},
	} {
		msgs, timer := liveSystem(t, tc.evt)
		if got := shape(msgs); !reflect.DeepEqual(got, []sysMsg{tc.want}) || !timer {
			t.Errorf("%s: got %+v (ChatTimer %v), want %+v and a ChatTimer", name, got, timer, tc.want)
		}
	}
}

func TestGroupMemberCannotSetTheTimerByProtocolMessage(t *testing.T) {
	c := systemClient()
	forged := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: sysGroup, Sender: jef, IsGroup: true}, Timestamp: sysAt},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(), EphemeralExpiration: proto.Uint32(0),
		}},
	}
	if !c.handleProtocol(forged) {
		t.Error("handleProtocol = false; want the forged setting consumed")
	}
	close(c.events)
	for ev := range c.events {
		t.Errorf("emitted %+v; want no event", ev)
	}
}

// stub is one History Sync stub of the group, by Jef unless participant says otherwise.
func stub(typ waWeb.WebMessageInfo_StubType, participant string, fromMe bool, params ...string) *waHistorySync.HistorySyncMsg {
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:                   &waCommon.MessageKey{RemoteJID: proto.String(sysGroup.String()), FromMe: proto.Bool(fromMe), ID: proto.String("stub-id")},
		MessageTimestamp:      proto.Uint64(uint64(sysAt.Unix())),
		Participant:           proto.String(participant),
		MessageStubType:       typ.Enum(),
		MessageStubParameters: params,
	}}
}

func historySystem(t *testing.T, chat types.JID, msgs ...*waHistorySync.HistorySyncMsg) []Message {
	t.Helper()
	batch, _ := systemClient().historySync(&waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{ID: proto.String(chat.String()), Messages: msgs}}})
	return batch.Chats[0].Messages
}

func TestHistorySyncStubsBecomeSystemMessages(t *testing.T) {
	j := jef.String()
	for _, tc := range []struct {
		name string
		stub *waHistorySync.HistorySyncMsg
		want sysMsg
	}{
		{"add", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, j, false, rob.String(), sysOwn.String()), sysMsg{j, false, System{Kind: SystemAdd, Targets: []string{rob.String(), ""}}}},
		{"join by link", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_INVITE, rob.String(), false, rob.String()), sysMsg{rob.String(), false, System{Kind: SystemInvite, Targets: []string{rob.String()}}}},
		{"leave", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE, piet.String(), false, piet.String()), sysMsg{piet.String(), false, System{Kind: SystemLeave, Targets: []string{piet.String()}}}},
		{"remove", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_REMOVE, j, false, piet.String()), sysMsg{j, false, System{Kind: SystemRemove, Targets: []string{piet.String()}}}},
		{"promote", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE, j, false, ann.String()), sysMsg{j, false, System{Kind: SystemPromote, Targets: []string{ann.String()}}}},
		{"demote you by LID", stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_DEMOTE, j, false, sysOwnL.String()), sysMsg{j, false, System{Kind: SystemDemote, Targets: []string{""}}}},
		{"subject by you", stub(waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, "", true, "Family"), sysMsg{sysOwn.String(), true, System{Kind: SystemSubject, Value: "Family"}}},
		{"description", stub(waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION, j, false, "new text"), sysMsg{j, false, System{Kind: SystemDescription}}},
		{"description deleted", stub(waWeb.WebMessageInfo_GROUP_CHANGE_DESCRIPTION, j, false), sysMsg{j, false, System{Kind: SystemDescription, Value: SystemDeleted}}},
		{"icon", stub(waWeb.WebMessageInfo_GROUP_CHANGE_ICON, j, false, "123"), sysMsg{j, false, System{Kind: SystemIcon}}},
		{"icon removed", stub(waWeb.WebMessageInfo_GROUP_CHANGE_ICON, j, false, "remove"), sysMsg{j, false, System{Kind: SystemIcon, Value: SystemDeleted}}},
		{"create", stub(waWeb.WebMessageInfo_GROUP_CREATE, j, false, "Family"), sysMsg{j, false, System{Kind: SystemCreate}}},
		{"timer", stub(waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING, j, false, "604800"), sysMsg{j, false, System{Kind: SystemTimer, Value: "604800"}}},
	} {
		got := historySystem(t, sysGroup, tc.stub)
		if s := shape(got); !reflect.DeepEqual(s, []sysMsg{tc.want}) {
			t.Errorf("%s: got %+v, want %+v", tc.name, s, tc.want)
			continue
		}
		if got[0].Time != sysAt.Unix() || got[0].ID == "stub-id" {
			t.Errorf("%s: Time %d, ID %q: want the stub time and a synthetic ID", tc.name, got[0].Time, got[0].ID)
		}
	}
}

func TestHistorySyncTimerStubInADirectChat(t *testing.T) {
	m := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:                   &waCommon.MessageKey{RemoteJID: proto.String(piet.String()), ID: proto.String("x")},
		MessageTimestamp:      proto.Uint64(uint64(sysAt.Unix())),
		MessageStubType:       waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING.Enum(),
		MessageStubParameters: []string{"86400"},
	}}
	want := []sysMsg{{piet.String(), false, System{Kind: SystemTimer, Value: "86400"}}}
	if got := shape(historySystem(t, piet, m)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestHistorySyncDropsOtherStubs(t *testing.T) {
	text := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:     &waCommon.MessageKey{RemoteJID: proto.String(sysGroup.String()), ID: proto.String("m1"), Participant: proto.String(jef.String())},
		Message: &waE2E.Message{Conversation: proto.String("hi")},
	}}
	got := historySystem(t, sysGroup, stub(waWeb.WebMessageInfo_GROUP_CHANGE_INVITE_LINK, jef.String(), false), text,
		stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, "", false, "not a jid@@"))
	if len(got) != 2 || got[0].ID != "m1" || got[1].System == nil {
		t.Fatalf("got %+v, want the text, then the add without its broken target", got)
	}
}

// The live event and its History Sync copy have one ID, also when the live actor is a LID and the stub holds the phone JID.
func TestLiveAndHistorySyncCopiesShareTheID(t *testing.T) {
	for name, pair := range map[string]struct {
		live any
		hist *waHistorySync.HistorySyncMsg
	}{
		"LID actor": {
			&events.GroupInfo{JID: sysGroup, Sender: &jefLID, SenderPN: &jef, Timestamp: sysAt, Name: &types.GroupName{Name: "Family"}},
			stub(waWeb.WebMessageInfo_GROUP_CHANGE_SUBJECT, jef.String(), false, "Family"),
		},
		"targets in another order, you by LID": {
			&events.GroupInfo{JID: sysGroup, Sender: &jef, Timestamp: sysAt, Join: []types.JID{sysOwnL, rob}},
			stub(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD, jef.String(), false, rob.String(), sysOwn.String()),
		},
		"by you": {
			&events.GroupInfo{JID: sysGroup, Sender: &sysOwnL, Timestamp: sysAt, Ephemeral: &types.GroupEphemeral{}},
			stub(waWeb.WebMessageInfo_CHANGE_EPHEMERAL_SETTING, "", true, "0"),
		},
	} {
		live, _ := liveSystem(t, pair.live)
		hist := historySystem(t, sysGroup, pair.hist)
		if len(live) != 1 || len(hist) != 1 || live[0].ID != hist[0].ID {
			t.Errorf("%s: live %+v and History Sync %+v must share one ID", name, live, hist)
		}
	}
	a, _ := liveSystem(t, &events.GroupInfo{JID: sysGroup, Sender: &jef, Timestamp: sysAt, Name: &types.GroupName{Name: "A"}})
	b, _ := liveSystem(t, &events.GroupInfo{JID: sysGroup, Sender: &jef, Timestamp: sysAt.Add(time.Second), Name: &types.GroupName{Name: "A"}})
	if a[0].ID == b[0].ID {
		t.Error("two changes one second apart share an ID")
	}
}

func TestLiveSubjectChangeIsCleaned(t *testing.T) {
	evt := &events.GroupInfo{JID: sysGroup, Sender: &jef, Timestamp: sysAt, Name: &types.GroupName{Name: "Fam\n‮ily"}}
	msgs, _ := liveSystem(t, evt)
	if len(msgs) != 1 || msgs[0].System.Value != "Family" {
		t.Errorf("got %+v, want one subject System Message with value Family", msgs)
	}
}
