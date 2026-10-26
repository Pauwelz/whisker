// Package receiptpolicy decides when the Daemon sends Read Receipts. It is pure logic.
// Opening a Chat clears its Read Marker in every mode: that is not part of this policy.
package receiptpolicy

import "github.com/Pauwelz/whisker/daemon/internal/settings"

// Trigger is the event that asks whether Read Receipts are due.
type Trigger int

const (
	// Attended: the Chat just became the Attended Chat.
	Attended Trigger = iota
	// Incoming: a new Message arrived in the Chat.
	Incoming
	// Interaction: the user sent a Message in the Chat.
	// A sent Message (also a Quote reply) and a Reaction of the user are interactions.
	// Edit, Revoke, typing and opening Media are not interactions: they never call Due.
	Interaction
)

// Due reports whether to send Read Receipts for the unread Messages of a Chat now.
// mode is a Read Receipts setting. attended is true when the Chat is the Attended Chat.
func Due(mode string, t Trigger, attended bool) bool {
	switch mode {
	case settings.ReadReceiptsOnOpen:
		return attended && (t == Attended || t == Incoming)
	case settings.ReadReceiptsOnInteraction:
		return t == Interaction
	}
	return false
}

// SelfDue reports whether opening a Chat sends read-self receipts for its unread Messages.
// A read-self receipt clears the Chat on the user's own devices, and the sender gets no Read Receipt.
// onOpen needs none: its Read Receipts reach the user's own devices too.
func SelfDue(mode string) bool { return mode == settings.ReadReceiptsOnInteraction }

// Played reports what to send when the user starts a voice note.
// played: send Played. read: also send Read for that Message only (never for the rest of the Chat).
func Played(mode string) (played, read bool) {
	switch mode {
	case settings.ReadReceiptsOnOpen:
		return true, false // Read went out when the Chat became attended
	case settings.ReadReceiptsOnInteraction:
		return true, true
	}
	return false, false
}
