package whatsapp

import (
	"errors"

	"go.mau.fi/whatsmeow"
)

// Typed reason codes for the failure logs. A log line holds one of these and never err.Error(): the text could hold hosts or names.

// Presence and connect codes. Any other error is CodeUnknown.
const (
	CodeUnknown          = "unknown"
	CodeNoPushName       = "no-push-name"
	CodeOffline          = "offline"
	CodeAlreadyConnected = "already-connected"
)

// DownloadStatus returns the HTTP status that failed a Media download, or 0 when the error holds none.
func DownloadStatus(err error) int {
	var he whatsmeow.DownloadHTTPError
	if errors.As(err, &he) && he.Response != nil {
		return he.StatusCode
	}
	return 0
}

// PresenceCode maps a presence error to one of the codes above.
func PresenceCode(err error) string {
	switch {
	case errors.Is(err, whatsmeow.ErrNoPushName):
		return CodeNoPushName
	case errors.Is(err, ErrOffline), errors.Is(err, whatsmeow.ErrNotConnected):
		return CodeOffline
	}
	return CodeUnknown
}

// ConnectCode maps a connect error to one of the codes above.
func ConnectCode(err error) string {
	if errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return CodeAlreadyConnected
	}
	return CodeUnknown
}
