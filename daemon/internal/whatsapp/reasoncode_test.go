package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/socket"
	"go.mau.fi/whatsmeow/store"
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
		{PresenceReason(fmt.Errorf("x: %w", whatsmeow.ErrNoPushName)), ReasonNoPushName},
		{PresenceReason(ErrOffline), ReasonOffline},
		{PresenceReason(errors.New("Alice")), ReasonUnknown},
		{ConnectReason(whatsmeow.ErrAlreadyConnected), ReasonAlreadyConnected},
		{ConnectReason(errors.New("dial example.com")), ReasonUnknown},
		{ConnectReason(fmt.Errorf("x: %w", store.ErrDeviceDeleted)), ReasonDeviceDeleted},
		{ConnectReason(fmt.Errorf("x: %w", socket.ErrDialFailed)), ReasonNetwork},
		{ConnectReason(context.DeadlineExceeded), ReasonNetwork},
		{ConnectReason(&net.OpError{Op: "dial", Err: errors.New("example.com")}), ReasonNetwork},
		{ConnectReason(socket.ErrWithStatusCode{StatusCode: 403}), ReasonRejected},
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
