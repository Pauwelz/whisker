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
	// Logout tells WhatsApp to remove this Companion Device, so the phone no longer lists it.
	// It needs a connection. On error the device stays linked.
	Logout() error
	// Disconnect closes the connection. It sends no event.
	Disconnect()
	// NewMessageID returns an unused Message ID. A send and its retries use one ID, so WhatsApp drops a repeat.
	NewMessageID() string
	// Send sends a text Message to a Chat. It returns ErrOffline when there is no connection.
	// It can block for a long time: call it off the Daemon loop.
	Send(chat, id, text string) error
	// Events delivers events in order. The channel stays open until the process ends.
	Events() <-chan Event
}

var ErrInvalidPhone = errors.New("invalid phone number")

// ErrOffline means Send found no connection. The Message is not sent.
var ErrOffline = errors.New("not connected")

// Delivery States. A Message of another Participant has none (empty).
const (
	StatePending   = "pending"
	StateSent      = "sent"
	StateDelivered = "delivered"
	StateRead      = "read"
	StatePlayed    = "played"
	StateFailed    = "failed"
)

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

// Chat kinds.
const (
	KindDirect = "direct"
	KindGroup  = "group"
	KindSelf   = "self"
)

// Message is one Message of a History Sync batch. Times are Unix seconds.
type Message struct {
	ID       string `json:"id"`
	Sender   string `json:"sender"` // JID of the author
	FromMe   bool   `json:"fromMe"`
	Time     int64  `json:"time"`
	Text     string `json:"text"`
	PushName string `json:"pushName"` // the author's Push Name as sent with the Message, may be empty
}

// HistoryChat is one Chat of a History Sync batch, with the App State that came with it.
// MutedUntil is Unix seconds: 0 is not muted, -1 is muted for ever.
type HistoryChat struct {
	JID        string    `json:"jid"`
	Kind       string    `json:"kind"`
	Subject    string    `json:"subject"` // the group name, empty for other kinds
	LastActive int64     `json:"lastActive"`
	Pinned     bool      `json:"pinned"`
	MutedUntil int64     `json:"mutedUntil"`
	Unread     bool      `json:"unread"`
	Messages   []Message `json:"messages"`
}

// HistorySync is one batch of History Sync. Batches can repeat a Chat with more Messages.
type HistorySync struct {
	Chats     []HistoryChat `json:"chats"`
	PushNames []PushName    `json:"pushNames"`
}

// PushName is the Push Name that a Participant set. An empty Name removes it.
type PushName struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// ContactName is the name that the phone contacts hold for a Participant. An empty Name removes it.
type ContactName struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// Pin, Mute and ReadMarker are App State changes of one Chat.
// Mute.Until is Unix seconds: 0 is not muted, -1 is muted for ever.
type Pin struct {
	JID    string `json:"jid"`
	Pinned bool   `json:"pinned"`
}

type Mute struct {
	JID   string `json:"jid"`
	Until int64  `json:"until"`
}

type ReadMarker struct {
	JID    string `json:"jid"`
	Unread bool   `json:"unread"`
}

// NewMessage is a Message that arrived live, or that another device of the user sent. Time is Unix seconds.
// Kind is the kind of the Chat, for a Chat that is not stored yet.
type NewMessage struct {
	Chat    string  `json:"chat"`
	Kind    string  `json:"kind"`
	Message Message `json:"message"`
}

// Receipt moves the Delivery State of the user's own Messages in a Direct Chat.
// State is StateDelivered, StateRead or StatePlayed.
type Receipt struct {
	Chat  string   `json:"chat"`
	IDs   []string `json:"ids"`
	State string   `json:"state"`
}

// Connected and Disconnected report the connection to WhatsApp.
type Connected struct{}
type Disconnected struct{}

func (NewMessage) isEvent()   {}
func (Receipt) isEvent()      {}
func (Connected) isEvent()    {}
func (Disconnected) isEvent() {}
func (QRCode) isEvent()      {}
func (PairSuccess) isEvent() {}
func (LinkFailed) isEvent()  {}
func (LoggedOut) isEvent()   {}
func (HistorySync) isEvent() {}
func (PushName) isEvent()    {}
func (ContactName) isEvent() {}
func (Pin) isEvent()         {}
func (Mute) isEvent()        {}
func (ReadMarker) isEvent()  {}

// Reasons for LinkFailed. docs/contract.md mirrors this list.
const (
	ReasonTimeout                   = "timeout"
	ReasonClientOutdated            = "client-outdated"
	ReasonScannedWithoutMultidevice = "scanned-without-multidevice"
	ReasonPairError                 = "pair-error"
	ReasonConnectFailed             = "connect-failed"
	ReasonUnexpected                = "unexpected"
)
