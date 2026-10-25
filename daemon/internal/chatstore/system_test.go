package chatstore

import (
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

func TestSystemTextOfEachKind(t *testing.T) {
	jef := systemActor{Name: "Jef Cools"}
	for _, tc := range []struct {
		kind    string
		actor   systemActor
		targets []string // "" is the user
		value   string
		want    string
	}{
		{whatsapp.SystemAdd, jef, []string{"Robert Maes", "Ann"}, "", "Jef Cools added Robert Maes and Ann"},
		{whatsapp.SystemAdd, jef, []string{"A", "B", "C"}, "", "Jef Cools added A, B and C"},
		{whatsapp.SystemAdd, jef, []string{""}, "", "Jef Cools added you"},
		{whatsapp.SystemInvite, systemActor{}, []string{"Robert Maes"}, "", "Robert Maes joined using an invite link"},
		{whatsapp.SystemInvite, systemActor{}, []string{""}, "", "You joined using an invite link"},
		{whatsapp.SystemLeave, systemActor{}, []string{"Piet"}, "", "Piet left"},
		{whatsapp.SystemLeave, systemActor{}, []string{"", "Piet"}, "", "You and Piet left"},
		{whatsapp.SystemRemove, jef, []string{"Piet"}, "", "Jef Cools removed Piet"},
		{whatsapp.SystemRemove, systemActor{Me: true}, []string{"Piet"}, "", "You removed Piet"},
		{whatsapp.SystemPromote, jef, []string{"Ann"}, "", "Jef Cools made Ann an admin"},
		{whatsapp.SystemPromote, jef, []string{"Ann", "Piet"}, "", "Jef Cools made Ann and Piet admins"},
		{whatsapp.SystemDemote, jef, []string{""}, "", "Jef Cools removed you as admin"},
		{whatsapp.SystemSubject, jef, nil, "Family", `Jef Cools changed the subject to "Family"`},
		{whatsapp.SystemDescription, jef, nil, "", "Jef Cools changed the group description"},
		{whatsapp.SystemDescription, jef, nil, whatsapp.SystemDeleted, "Jef Cools deleted the group description"},
		{whatsapp.SystemIcon, jef, nil, "", "Jef Cools changed the group icon"},
		{whatsapp.SystemIcon, jef, nil, whatsapp.SystemDeleted, "Jef Cools removed the group icon"},
		{whatsapp.SystemCreate, jef, nil, "", "Jef Cools created the group"},
		{whatsapp.SystemTimer, jef, nil, "604800", "Jef Cools set the disappearing timer to 7 days"},
		{whatsapp.SystemTimer, jef, nil, "86400", "Jef Cools set the disappearing timer to 24 hours"},
		{whatsapp.SystemTimer, jef, nil, "7776000", "Jef Cools set the disappearing timer to 90 days"},
		{whatsapp.SystemTimer, systemActor{Me: true}, nil, "0", "You turned off disappearing messages"},
	} {
		if got := systemText(tc.kind, tc.actor, tc.targets, tc.value); got != tc.want {
			t.Errorf("systemText(%s) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
