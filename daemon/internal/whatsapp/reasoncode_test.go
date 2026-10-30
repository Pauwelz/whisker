package whatsapp

import (
	"errors"
	"fmt"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func TestReasonCodes(t *testing.T) {
	wrapped := fmt.Errorf("%w: %w", ErrMediaExpired, whatsmeow.ErrMediaDownloadFailedWith410)
	if got := DownloadStatus(wrapped); got != 410 {
		t.Errorf("DownloadStatus = %d, want 410", got)
	}
	if got := DownloadStatus(errors.New("host example.com")); got != 0 {
		t.Errorf("DownloadStatus of a plain error = %d, want 0", got)
	}
	for _, tc := range []struct {
		got, want string
	}{
		{PresenceCode(fmt.Errorf("x: %w", whatsmeow.ErrNoPushName)), CodeNoPushName},
		{PresenceCode(ErrOffline), CodeOffline},
		{PresenceCode(errors.New("Alice")), CodeUnknown},
		{ConnectCode(whatsmeow.ErrAlreadyConnected), CodeAlreadyConnected},
		{ConnectCode(errors.New("dial example.com")), CodeUnknown},
	} {
		if tc.got != tc.want {
			t.Errorf("code = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestDisconnectedCarriesTheCode(t *testing.T) {
	c := systemClient()
	c.handle(&events.TemporaryBan{Code: events.TempBanBlockedByUsers})
	if got := (<-c.events).(Disconnected); got.Code != 102 {
		t.Errorf("TemporaryBan Code = %d, want 102", got.Code)
	}
	c.handle(&events.ConnectFailure{Reason: events.ConnectFailureLoggedOut})
	if got := (<-c.events).(Disconnected); got.Code != 401 {
		t.Errorf("ConnectFailure Code = %d, want 401", got.Code)
	}
}
