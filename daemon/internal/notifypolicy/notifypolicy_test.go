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
		{"Attended Chat", Input{Notifications: true, Attended: true}, false},
		{"own Message from another device", Input{Notifications: true, FromMe: true}, false},
	}
	for _, c := range cases {
		if got := Notifies(c.in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
