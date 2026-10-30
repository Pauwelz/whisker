package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/socket"
	"go.mau.fi/whatsmeow/store"
)

// Typed reasons for the failure logs. A log line holds one of these and never err.Error(): the text could hold hosts or names.

// Presence and connect reasons. Any other error is ReasonUnknown.
const (
	ReasonUnknown          = "unknown"
	ReasonNoPushName       = "no-push-name"
	ReasonOffline          = "offline"
	ReasonAlreadyConnected = "already-connected"
	ReasonDeviceDeleted    = "device-deleted"
	ReasonNetwork          = "network"
	ReasonRejected         = "rejected"
)

// DownloadStatus returns the HTTP status that failed a Media download, or 0 when the error holds none.
func DownloadStatus(err error) int {
	var he whatsmeow.DownloadHTTPError
	if errors.As(err, &he) && he.Response != nil {
		return he.StatusCode
	}
	return 0
}

// markExpired wraps a download error whose HTTP status says the link is dead (403, 404 or 410) in ErrMediaExpired. Other errors stay as they are.
func markExpired(err error) error {
	switch DownloadStatus(err) {
	case 403, 404, 410:
		return fmt.Errorf("%w: %w", ErrMediaExpired, err)
	}
	return err
}

// PresenceReason maps a presence error to one of the reasons above.
func PresenceReason(err error) string {
	switch {
	case errors.Is(err, whatsmeow.ErrNoPushName):
		return ReasonNoPushName
	case errors.Is(err, ErrOffline), errors.Is(err, whatsmeow.ErrNotConnected):
		return ReasonOffline
	}
	return ReasonUnknown
}

// ConnectReason maps a connect error to one of the reasons above. It checks types only, never the text.
func ConnectReason(err error) string {
	var netErr net.Error
	var status socket.ErrWithStatusCode
	switch {
	case errors.Is(err, whatsmeow.ErrAlreadyConnected):
		return ReasonAlreadyConnected
	case errors.Is(err, store.ErrDeviceDeleted):
		return ReasonDeviceDeleted
	case errors.Is(err, socket.ErrDialFailed), errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr):
		return ReasonNetwork
	case errors.As(err, &status): // WhatsApp answered the websocket request with an HTTP status
		return ReasonRejected
	}
	return ReasonUnknown
}
