// Package whatsapp is the WhatsApp port: a small interface over the WhatsApp client.
// Later tickets add methods (sends, receipts, Presence) and event kinds here.
// Only the adapter in client.go imports whatsmeow, and it maps whatsmeow types to these types.
package whatsapp

import "errors"

// Port is what the Daemon needs from WhatsApp.
type Port interface {
	// Linked reports whether this Profile is a Companion Device.
	Linked() bool
	// Connect connects to WhatsApp. An unlinked Profile starts linking: QRCode events follow.
	// Calling it again restarts linking.
	Connect() error
	// PairCode asks for a Pair Code for the phone number (digits, international, no "+").
	// Call it after Connect on an unlinked Profile. It returns ErrInvalidPhone for a bad number.
	PairCode(phone string) (string, error)
	// Disconnect closes the connection. It sends no event.
	Disconnect()
	// Events delivers events in order. The channel stays open until the process ends.
	Events() <-chan Event
}

var ErrInvalidPhone = errors.New("invalid phone number")

// Event is a sealed sum type: the kinds below are all of them.
type Event interface{ isEvent() }

// QRCode carries the raw code string. A new QRCode replaces the old one while the user waits.
type QRCode struct{ Code string }

// PairSuccess means the phone authorized this Companion Device.
type PairSuccess struct{}

// LinkFailed means linking stopped. Reason is one of the Reason constants.
type LinkFailed struct{ Reason string }

// LoggedOut means the phone removed this Companion Device.
type LoggedOut struct{}

func (QRCode) isEvent()      {}
func (PairSuccess) isEvent() {}
func (LinkFailed) isEvent()  {}
func (LoggedOut) isEvent()   {}

// Reasons for LinkFailed. docs/contract.md mirrors this list.
const (
	ReasonTimeout                   = "timeout"
	ReasonClientOutdated            = "client-outdated"
	ReasonScannedWithoutMultidevice = "scanned-without-multidevice"
	ReasonPairError                 = "pair-error"
	ReasonConnectFailed             = "connect-failed"
	ReasonUnexpected                = "unexpected"
)
