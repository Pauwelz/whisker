package notifypolicy

import "testing"

func TestNotifies(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want bool
	}{
		{"new Message", Input{Notifications: true}, true},
		{"notifications off", Input{}, false},
		{"Muted Chat", Input{Notifications: true, Muted: true}, false},
		{"mention in a Muted Chat", Input{Notifications: true, Muted: true, Mentioned: true}, true},
		{"mention in the Attended Chat", Input{Notifications: true, Attended: true, Mentioned: true}, false},
		{"mention with notifications off", Input{Mentioned: true}, false},
		{"archived Chat", Input{Notifications: true, Archived: true}, false},
		{"mention in an archived Chat", Input{Notifications: true, Archived: true, Mentioned: true}, true},
		{"Attended Chat", Input{Notifications: true, Attended: true}, false},
		{"own Message from another device", Input{Notifications: true, FromMe: true}, false},
	}
	for _, c := range cases {
		if got := Notifies(c.in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
