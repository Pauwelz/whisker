package whatsapp

import (
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestResolvePhone(t *testing.T) {
	lid := types.JID{User: "111", Server: types.HiddenUserServer}
	unknown := types.JID{User: "222", Server: types.HiddenUserServer}
	pn := types.JID{User: "3247", Server: types.DefaultUserServer}
	other := types.JID{User: "3299", Server: types.DefaultUserServer}
	group := types.JID{User: "12-34", Server: types.GroupServer}
	store := func(j types.JID) (types.JID, error) {
		if j.User == "111" {
			return pn, nil
		}
		return types.EmptyJID, errors.New("unknown")
	}
	none := func(types.JID) (types.JID, error) { return types.EmptyJID, errors.New("unknown") }
	tests := []struct {
		name   string
		jid    types.JID
		lookup func(types.JID) (types.JID, error)
		alts   []types.JID
		want   string
	}{
		{"phone JID stays", pn, store, nil, "3247@s.whatsapp.net"},
		{"group stays", group, store, nil, "12-34@g.us"},
		{"device part is dropped", types.JID{User: "3247", Device: 3, Server: types.DefaultUserServer}, store, nil, "3247@s.whatsapp.net"},
		{"alt wins over the store", lid, none, []types.JID{pn}, "3247@s.whatsapp.net"},
		{"empty and LID alts are skipped", lid, store, []types.JID{types.EmptyJID, lid}, "3247@s.whatsapp.net"},
		{"store lookup", lid, store, nil, "3247@s.whatsapp.net"},
		{"alt for a LID that the store lacks", unknown, store, []types.JID{other}, "3299@s.whatsapp.net"},
		{"unknown LID stays", unknown, store, nil, "222@lid"},
	}
	for _, tc := range tests {
		if got := resolvePhone(tc.jid, tc.lookup, tc.alts...).String(); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
