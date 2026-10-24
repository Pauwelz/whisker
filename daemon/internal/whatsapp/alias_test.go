package whatsapp

import (
	"slices"
	"testing"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
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
