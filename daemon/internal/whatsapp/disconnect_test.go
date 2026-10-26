package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/types/events"
)

// whatsmeow ends the session on these events and does not reconnect. Dropped, the Plugin showed no Offline banner.
func TestSessionEndingEventsBecomeDisconnectedWithAReason(t *testing.T) {
	for _, tc := range []struct {
		evt  any
		want string
	}{
		{&events.StreamReplaced{}, DisconnectReplaced},
		{&events.TemporaryBan{}, DisconnectTemporaryBan},
		{&events.ConnectFailure{}, DisconnectConnectFailure},
		{&events.Disconnected{}, ""},
	} {
		c := systemClient()
		c.handle(tc.evt)
		if got := (<-c.events).(Disconnected); got.Reason != tc.want {
			t.Errorf("%T: Reason = %q, want %q", tc.evt, got.Reason, tc.want)
		}
	}
}
