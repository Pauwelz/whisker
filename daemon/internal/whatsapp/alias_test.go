package whatsapp

import (
	"slices"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
)

func TestAliasOfNeedsALIDAndAPhoneJID(t *testing.T) {
	lid := types.JID{User: "99", Server: types.HiddenUserServer, Device: 3}
	pn := types.JID{User: "3160", Server: types.DefaultUserServer}
	if a, ok := aliasOf(lid, pn); !ok || a != (ChatAlias{LID: "99@lid", Phone: "3160@s.whatsapp.net"}) {
		t.Errorf("aliasOf = %+v, %v", a, ok)
	}
	for _, pair := range [][2]types.JID{{pn, pn}, {lid, lid}, {lid, types.EmptyJID}, {pn, lid}} {
		if _, ok := aliasOf(pair[0], pair[1]); ok {
			t.Errorf("aliasOf(%v, %v) must fail", pair[0], pair[1])
		}
	}
}

func TestHistoryAliasesReadsEveryMapping(t *testing.T) {
	data := &waHistorySync.HistorySync{PhoneNumberToLidMappings: []*waHistorySync.PhoneNumberToLIDMapping{
		{LidJID: new("99@lid"), PnJID: new("3160@s.whatsapp.net")},
		{LidJID: new("98@lid"), PnJID: new("3161@c.us")},
		{LidJID: new("not a jid@"), PnJID: new("3162@s.whatsapp.net")},
		{LidJID: new("97@lid")},
	}}
	want := []ChatAlias{{"99@lid", "3160@s.whatsapp.net"}, {"98@lid", "3161@s.whatsapp.net"}}
	if got := historyAliases(data); !slices.Equal(got, want) {
		t.Errorf("historyAliases = %+v; want %+v", got, want)
	}
}

func TestHistoryAliasesReadsConversationsThatCarryAPhone(t *testing.T) {
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
		{ID: new("99@lid"), PnJID: new("3160@s.whatsapp.net")},
		{ID: new("98@lid")},
		{ID: new("3161@s.whatsapp.net"), PnJID: new("3161@s.whatsapp.net")},
	}}
	want := []ChatAlias{{"99@lid", "3160@s.whatsapp.net"}}
	if got := historyAliases(data); !slices.Equal(got, want) {
		t.Errorf("historyAliases = %+v; want %+v", got, want)
	}
}

func TestLiveAliasesReadsTheGroupSender(t *testing.T) {
	group := types.JID{User: "120363", Server: types.GroupServer}
	lid := types.JID{User: "232151723315295", Server: types.HiddenUserServer, Device: 4}
	pn := types.JID{User: "32470", Server: types.DefaultUserServer}
	want := []ChatAlias{{"232151723315295@lid", "32470@s.whatsapp.net"}}
	for _, src := range []types.MessageSource{
		{Chat: group, Sender: lid, SenderAlt: pn, IsGroup: true},
		{Chat: group, Sender: pn, SenderAlt: lid, IsGroup: true},
	} {
		if got := liveAliases(src, group); !slices.Equal(got, want) {
			t.Errorf("liveAliases(%+v) = %+v; want %+v", src, got, want)
		}
	}
}

func TestLiveAliasesReadsADirectChatOnce(t *testing.T) {
	lid := types.JID{User: "99", Server: types.HiddenUserServer}
	pn := types.JID{User: "3160", Server: types.DefaultUserServer}
	src := types.MessageSource{Chat: lid, Sender: lid, SenderAlt: pn}
	want := []ChatAlias{{"99@lid", "3160@s.whatsapp.net"}}
	if got := liveAliases(src, pn); !slices.Equal(got, want) {
		t.Errorf("liveAliases = %+v; want %+v", got, want)
	}
}

func TestContactBookKeepsNamedContactsUnderThePhone(t *testing.T) {
	lid := types.JID{User: "295", Server: types.HiddenUserServer}
	pn := types.JID{User: "32470", Server: types.DefaultUserServer}
	other := types.JID{User: "31600", Server: types.DefaultUserServer}
	unnamed := types.JID{User: "31700", Server: types.DefaultUserServer}
	contacts := map[types.JID]types.ContactInfo{
		lid:     {FullName: "Jef Cools"},
		other:   {FullName: "Ann"},
		unnamed: {PushName: "Push only"},
	}
	phone := func(j types.JID) types.JID {
		if j == lid {
			return pn
		}
		return j
	}
	want := ContactBook{Names: []ContactName{{"31600@s.whatsapp.net", "Ann"}, {"32470@s.whatsapp.net", "Jef Cools"}}}
	if got := contactBook(contacts, phone); !slices.Equal(got.Names, want.Names) {
		t.Errorf("contactBook = %+v; want %+v", got, want)
	}
}

func TestHistoryReactionsMapsTheReactor(t *testing.T) {
	phone := func(s string) string {
		if s == "295@lid" {
			return "32470@s.whatsapp.net"
		}
		return s
	}
	rs := []*waWeb.Reaction{
		{Key: &waCommon.MessageKey{FromMe: new(true), RemoteJID: new("g1@g.us")}, Text: new("❤️")},
		{Key: &waCommon.MessageKey{RemoteJID: new("g1@g.us"), Participant: new("295@lid")}, Text: new("😂")},
		{Key: &waCommon.MessageKey{RemoteJID: new("31600@s.whatsapp.net")}, Text: new("👍")},
		{Key: &waCommon.MessageKey{RemoteJID: new("g1@g.us"), Participant: new("1@s.whatsapp.net")}, Text: new("")}, // removed
	}
	want := []HistoryReaction{{"", true, "❤️"}, {"32470@s.whatsapp.net", false, "😂"}, {"31600@s.whatsapp.net", false, "👍"}}
	if got := historyReactions(rs, phone); !slices.Equal(got, want) {
		t.Errorf("historyReactions = %+v; want %+v", got, want)
	}
}
