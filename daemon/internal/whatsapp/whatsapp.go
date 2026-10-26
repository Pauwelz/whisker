// Package whatsapp is the WhatsApp port: a small interface over the WhatsApp client.
// Later tickets add methods (sends, receipts, Presence) and event kinds here.
// Only the adapter (client.go, send.go, events.go, content.go and system.go) imports whatsmeow here, and it maps whatsmeow types to these types.
// The device store (internal/profile) and the Media crypto (internal/media) import small whatsmeow parts.
package whatsapp

import (
	"errors"
	"os"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

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
	// Lookup asks WhatsApp whether the phone number (international, any separators) is on WhatsApp.
	// It returns the digits and the person's WhatsApp ID, or "" for a number that is not on WhatsApp.
	// It returns ErrInvalidPhone for a bad number, and ErrOffline when there is no connection.
	Lookup(phone string) (digits, jid string, err error)
	// ProfilePicture fetches the small Profile picture (JPEG) of a Chat or group icon. knownID is the picture ID that the caller holds.
	// It returns knownID and no data when the picture did not change, and "" and no data when there is no picture or WhatsApp hides it.
	// It returns ErrOffline when there is no connection. It can block for a long time: call it off the Daemon loop.
	ProfilePicture(chat, knownID string) (id string, data []byte, err error)
	// PhoneOf is the phone JID that the local LID store holds for a LID, or "". It makes no network call.
	PhoneOf(lid string) string
	// RequestHistory asks the phone for up to count Messages of a Chat that are older than the anchor (On-Demand History Sync).
	// The answer comes later as a HistorySync event with OnDemand set. It returns ErrOffline when there is no connection.
	// It can block for a long time: call it off the Daemon loop.
	RequestHistory(chat string, anchor HistoryAnchor, count int) error
	// Logout tells WhatsApp to remove this Companion Device, so the phone no longer lists it.
	// It needs a connection. On error the device stays linked.
	Logout() error
	// Disconnect closes the connection. It sends no event.
	Disconnect()
	// NewMessageID returns an unused Message ID. A send and its retries use one ID, so WhatsApp drops a repeat.
	NewMessageID() string
	// Send sends a text Message to a Chat, as a reply to quote when quote is not nil.
	// It returns ErrOffline when there is no connection.
	// timer is the Disappearing timer in seconds: 0 sends a Message that stays.
	// It can block for a long time: call it off the Daemon loop.
	Send(chat, id, text string, quote *Quote, timer int64) error
	// SendMedia uploads the plain Media in plain and sends it with the caption. Call it off the Daemon loop.
	// It returns ErrOffline when there is no connection.
	SendMedia(chat, id string, m Media, plain *os.File, caption string, timer int64) error
	// React sets the Reaction of the user on a Message. An empty emoji removes it.
	// sender is the author of the Message ("" for the user). It returns ErrOffline when there is no connection.
	React(chat, id, sender, emoji string) error
	// Download downloads the plain Media into dst, and checks it against its hashes.
	// It can block for a long time: call it off the Daemon loop.
	Download(ref Media, dst *os.File) error
	// DownloadThumbnail downloads the separate thumbnail of Media (ThumbDirectPath), and checks it against its hashes.
	DownloadThumbnail(ref Media) ([]byte, error)
	// MarkRead sends a Read Receipt for Messages of one sender in a Chat. In a Group Chat sender is the author.
	// It returns ErrOffline when there is no connection.
	MarkRead(chat, sender string, ids []string) error
	// MarkPlayed sends a Played receipt for one voice note. In a Group Chat sender is the author.
	// It returns ErrOffline when there is no connection.
	MarkPlayed(chat, sender, id string) error
	// MarkReadSelf tells the user's own devices that the user read these Messages of one sender. The sender gets no Read Receipt.
	MarkReadSelf(chat, sender string, ids []string) error
	// MarkChatRead clears the Read Marker of a Chat on the user's devices (App State). It needs a connection.
	MarkChatRead(chat string) error
	// SetPinned pins or unpins a Chat on the user's devices (App State). It returns ErrOffline when there is no connection.
	SetPinned(chat string, pinned bool) error
	// SetMuted mutes a Chat until the Unix second until (App State): -1 for ever, 0 unmutes.
	// It returns ErrOffline when there is no connection.
	SetMuted(chat string, until int64) error
	// SetUnread sets or clears the Read Marker of a Chat on the user's devices (App State). It sends no Read Receipt.
	// It returns ErrOffline when there is no connection.
	SetUnread(chat string, unread bool) error
	// Edit replaces the text of a Message of the user. It returns ErrOffline when there is no connection.
	Edit(chat, id, text string) error
	// Revoke takes back a Message of the user for everyone. It returns ErrOffline when there is no connection.
	Revoke(chat, id string) error
	// DeleteForMe deletes a Message on the user's devices only (App State). sender is the author, "" for the user.
	// time is the Unix second of the Message. It returns ErrOffline when there is no connection.
	DeleteForMe(chat, id, sender string, fromMe bool, time int64) error
	// SendPresence tells WhatsApp that the user is online (true) or not. WhatsApp only sends Presence after it.
	SendPresence(online bool) error
	// SubscribePresence asks WhatsApp for the Presence of a Participant.
	SubscribePresence(jid string) error
	// SendTyping tells a Chat that the user is composing (true) or has paused (false).
	SendTyping(chat string, composing bool) error
	// GroupParticipants lists the Participants of a Group Chat, without the user. It returns ErrOffline when there is no connection.
	// It can block for a long time: call it off the Daemon loop.
	GroupParticipants(chat string) ([]Participant, error)
	// OwnUsers lists the user parts of the user's own IDs (phone number and LID). It is empty while unlinked.
	OwnUsers() []string
	// Events delivers events in order. The channel stays open until the process ends.
	Events() <-chan Event
}

var ErrInvalidPhone = errors.New("invalid phone number")

// Digits keeps the digits of a phone number, whatever the separators. It is the one digit rule of the Daemon.
func Digits(phone string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
}

// cleanPhone keeps the digits of a phone number. It returns ErrInvalidPhone for a number that is too short
// or not international. The rule is the one of whatsmeow's PairPhone. state/NewChat.js mirrors it (isLookupPhone).
func cleanPhone(phone string) (string, error) {
	digits := Digits(phone)
	if len(digits) <= 6 || strings.HasPrefix(digits, "0") {
		return "", ErrInvalidPhone
	}
	return digits, nil
}

// MaxThumbnailBytes caps a thumbnail, inline or separate. A thumbnail is a few KiB, so a larger body is not one.
const MaxThumbnailBytes = 256 << 10

// ErrThumbnailTooLarge means a thumbnail is above MaxThumbnailBytes. Code drops such a thumbnail and never cuts it.
var ErrThumbnailTooLarge = errors.New("thumbnail too large")

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

// Media is what a Message carries to fetch its file later. Key, DirectPath and the hashes stay in the chat store.
type Media struct {
	Kind          media.Kind `json:"kind"`
	Mime          string     `json:"mime"`
	Name          string     `json:"name"` // the file name of a document, else empty
	Size          int64      `json:"size"`
	Key           []byte     `json:"key"`
	DirectPath    string     `json:"directPath"`
	FileSHA256    []byte     `json:"fileSha256"`
	FileEncSHA256 []byte     `json:"fileEncSha256"`
	Thumbnail     []byte     `json:"thumbnail"` // the JPEG thumbnail WhatsApp sent with the Message, empty when none
	Duration      int64      `json:"duration"`  // seconds of audio, voice and video, 0 when WhatsApp sent none
	Gif           bool       `json:"gif"`       // a video that WhatsApp plays as a looping GIF
	// The link to the thumbnail that WhatsApp keeps apart from the Message. Empty when it sent none.
	// The thumbnail is encrypted with Key, as the file itself.
	ThumbDirectPath string `json:"thumbDirectPath"`
	ThumbSHA256     []byte `json:"thumbSha256"`
	ThumbEncSHA256  []byte `json:"thumbEncSha256"`
}

// Message is one Message of a History Sync batch. Times are Unix seconds.
// Text is the text, or the caption of Media. Media is nil for a text Message.
type Message struct {
	ID       string `json:"id"`
	Sender   string `json:"sender"` // JID of the author
	FromMe   bool   `json:"fromMe"`
	Time     int64  `json:"time"`
	Text     string `json:"text"`
	PushName string `json:"pushName"` // the author's Push Name as sent with the Message, may be empty
	Quote    *Quote `json:"quote"`    // the Message that this one replies to, nil for none
	Media    *Media `json:"media"`
	// Mentions are the JIDs that Text mentions as "@<user part>". MentionsMe tells that one of them is the user.
	Mentions   []string `json:"mentions"`
	MentionsMe bool     `json:"mentionsMe"`
	Timer      int64    `json:"timer"` // the Disappearing timer in seconds that the Message carries, 0 for none
	// Reactions are the Reactions that History Sync carries on the Message. Live Reactions come as Reaction events.
	Reactions []HistoryReaction `json:"reactions"`
	// State is the Delivery State that History Sync carries on a Message of the user. It is empty for a live Message.
	State string `json:"state"`
	// System is set for a System Message: Sender and FromMe name the actor, Text is empty, and the ID is SystemID.
	System *System `json:"system"`
}

// System is the change to a Chat that a System Message records. Kind is one of the System kinds.
// Targets are phone JIDs of Participants (a LID when no phone JID is known), and "" for the user.
// Value is the new subject, the timer in seconds, or SystemDeleted for a deleted description or icon.
type System struct {
	Kind    string   `json:"kind"`
	Targets []string `json:"targets"`
	Value   string   `json:"value"`
}

// System kinds. The timer kind applies to every Chat, the others to Group Chats only.
const (
	SystemAdd         = "add"
	SystemInvite      = "invite" // joined using an invite link
	SystemLeave       = "leave"
	SystemRemove      = "remove"
	SystemPromote     = "promote"
	SystemDemote      = "demote"
	SystemSubject     = "subject"
	SystemDescription = "description"
	SystemIcon        = "icon"
	SystemCreate      = "create"
	SystemTimer       = "timer"
)

// SystemDeleted is the Value of a description or icon that was deleted.
const SystemDeleted = "deleted"

// HistoryReaction is one Reaction on a Message of History Sync. Sender is the reactor, empty when FromMe.
type HistoryReaction struct {
	Sender string `json:"sender"`
	FromMe bool   `json:"fromMe"`
	Emoji  string `json:"emoji"`
}

// Quote is the Message that a reply quotes. Sender is its author, empty for the user. Text is the preview.
type Quote struct {
	ID     string `json:"id"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

// HistoryChat is one Chat of a History Sync batch, with the App State that came with it.
// MutedUntil is Unix seconds: 0 is not muted, -1 is muted for ever.
type HistoryChat struct {
	JID        string    `json:"jid"`
	Kind       string    `json:"kind"`
	Subject    string    `json:"subject"` // the group name, empty for other kinds
	LastActive int64     `json:"lastActive"`
	Pinned     bool      `json:"pinned"`
	Archived   bool      `json:"archived"`
	MutedUntil int64     `json:"mutedUntil"`
	Unread     bool      `json:"unread"`
	Timer      int64     `json:"timer"` // the Disappearing timer of the Chat in seconds, 0 for off
	Messages   []Message `json:"messages"`
}

// HistorySync is one batch of History Sync. Batches can repeat a Chat with more Messages.
type HistorySync struct {
	Chats     []HistoryChat `json:"chats"`
	PushNames []PushName    `json:"pushNames"`
	// OnDemand is true for the answer to RequestHistory. The answer names no request, only its Chats.
	OnDemand bool `json:"onDemand"`
}

// HistoryAnchor is the oldest stored Message of a Chat. An On-Demand History Sync asks for the Messages before it.
type HistoryAnchor struct {
	ID     string `json:"id"`
	FromMe bool   `json:"fromMe"`
	Time   int64  `json:"time"`
}

// PushName is the Push Name that a Participant set. An empty Name removes it.
type PushName struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// ContactBook is every name that the phone contacts hold, as whatsmeow stored them. It never removes a name.
// whatsmeow sends no ContactName events for a full App State sync, so the port reads its store instead.
type ContactBook struct {
	Names []ContactName `json:"names"`
}

// ContactName is the name that the phone contacts hold for a Participant. An empty Name removes it.
type ContactName struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// Pin, Archive, Mute and ReadMarker are App State changes of one Chat.
// Mute.Until is Unix seconds: 0 is not muted, -1 is muted for ever.
type Pin struct {
	JID    string `json:"jid"`
	Pinned bool   `json:"pinned"`
}

type Archive struct {
	JID      string `json:"jid"`
	Archived bool   `json:"archived"`
}

// PictureChanged says that the Profile picture of a Chat changed or went away. It carries no picture.
type PictureChanged struct {
	JID string `json:"jid"`
}

// UnarchiveChats is the App State setting "Keep chats archived". On means a new Message unarchives its Chat.
type UnarchiveChats struct {
	On bool `json:"on"`
}

type Mute struct {
	JID   string `json:"jid"`
	Until int64  `json:"until"`
}

type ReadMarker struct {
	JID    string `json:"jid"`
	Unread bool   `json:"unread"`
}

// ReadElsewhere says that the user read these incoming Messages of a Chat on another of their own devices.
// That device sent the Read Receipt, so whisker sends none.
type ReadElsewhere struct {
	Chat string   `json:"chat"`
	IDs  []string `json:"ids"`
}

// AppStateBatch holds the Pin, Archive, UnarchiveChats, Mute and ReadMarker events of one full App State sync, in order.
type AppStateBatch struct {
	Events []Event `json:"events"`
}

// NewMessage is a Message that arrived live, or that another device of the user sent. Time is Unix seconds.
// Kind is the kind of the Chat, for a Chat that is not stored yet.
type NewMessage struct {
	Chat    string  `json:"chat"`
	Kind    string  `json:"chatKind"`
	Message Message `json:"message"`
}

// Participant is a Participant of a Group Chat. WhatsApp can give either ID, so a Participant holds both. An unknown ID is empty.
type Participant struct {
	Phone string `json:"phone"` // phone JID, no device part
	LID   string `json:"lid"`   // LID, no device part
}

// Receipt moves the Delivery State of the user's own Messages.
// State is StateDelivered, StateRead or StatePlayed.
// Sender is set in a Group Chat only: the phone JID or LID of the Participant, without the device part. It is never a device of the user.
type Receipt struct {
	Chat   string   `json:"chat"`
	IDs    []string `json:"ids"`
	State  string   `json:"state"`
	Sender string   `json:"sender"`
}

// MessageEdited means a Participant, or another device of the user, changed the text of a stored Message.
// Sender and FromMe name the editor: only the author can edit a Message.
type MessageEdited struct {
	Chat   string `json:"chat"`
	ID     string `json:"id"`
	Sender string `json:"sender"`
	FromMe bool   `json:"fromMe"`
	Text   string `json:"text"`
}

// MessageRevoked means a stored Message was revoked for everyone.
type MessageRevoked struct {
	Chat string `json:"chat"`
	ID   string `json:"id"`
}

// Reaction means a Participant, or another device of the user, set or removed a Reaction on a stored Message.
// Sender and FromMe name the reactor. An empty Emoji removes it.
type Reaction struct {
	Chat   string `json:"chat"`
	ID     string `json:"id"`
	Sender string `json:"sender"`
	FromMe bool   `json:"fromMe"`
	Emoji  string `json:"emoji"`
}

// Presence is the availability of a Participant of a Direct Chat. LastSeen is Unix seconds, 0 when WhatsApp hides it.
type Presence struct {
	JID      string `json:"jid"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"lastSeen"`
}

// Activity states of a ChatPresence. Paused ends Composing and Recording.
const (
	ActivityComposing = "composing"
	ActivityRecording = "recording"
	ActivityPaused    = "paused"
)

// ChatPresence is the activity of a Participant in a Chat. State is one of the Activity constants.
type ChatPresence struct {
	Chat   string `json:"chat"`
	Sender string `json:"sender"`
	State  string `json:"state"`
}

// ChatTimer means the Disappearing timer of a Chat changed. Seconds is 0 when the timer is off.
// It applies to Messages from now on.
type ChatTimer struct {
	Chat    string `json:"chat"`
	Seconds int64  `json:"seconds"`
}

// ChatAlias says that a LID and a phone JID are the same Participant. LID is a "@lid" JID, Phone a "@s.whatsapp.net" JID.
// WhatsApp can send the mapping after the Chat that it concerns. The chat store then merges the LID Chat into the phone Chat.
// The same alias can come more than once.
type ChatAlias struct {
	LID   string `json:"lid"`
	Phone string `json:"phone"`
}

// Connected and Disconnected report the connection to WhatsApp.
type Connected struct{}
type Disconnected struct{}

func (NewMessage) isEvent()     {}
func (Receipt) isEvent()        {}
func (ChatTimer) isEvent()      {}
func (ChatAlias) isEvent()      {}
func (Presence) isEvent()       {}
func (ChatPresence) isEvent()   {}
func (MessageEdited) isEvent()  {}
func (MessageRevoked) isEvent() {}
func (Reaction) isEvent()       {}
func (Connected) isEvent()      {}
func (Disconnected) isEvent()   {}
func (QRCode) isEvent()         {}
func (PairSuccess) isEvent()    {}
func (LinkFailed) isEvent()     {}
func (LoggedOut) isEvent()      {}
func (HistorySync) isEvent()    {}
func (PushName) isEvent()       {}
func (ContactName) isEvent()    {}
func (ContactBook) isEvent()    {}
func (Pin) isEvent()            {}
func (Archive) isEvent()        {}
func (UnarchiveChats) isEvent() {}
func (Mute) isEvent()           {}
func (ReadMarker) isEvent()     {}
func (ReadElsewhere) isEvent()  {}
func (AppStateBatch) isEvent()  {}
func (PictureChanged) isEvent() {}

// Reasons for LinkFailed. docs/contract.md mirrors this list.
const (
	ReasonTimeout                   = "timeout"
	ReasonClientOutdated            = "client-outdated"
	ReasonScannedWithoutMultidevice = "scanned-without-multidevice"
	ReasonPairError                 = "pair-error"
	ReasonConnectFailed             = "connect-failed"
	ReasonUnexpected                = "unexpected"
)
