// Package attention holds what the Plugin tells the Daemon about the pop-out: open or closed, and which Chat it shows.
// The Daemon derives the Attended Chat from it. Presence (pop-out open) and notifications (Attended Chat) read it here.
package attention

// Attention is the pop-out state. It is not safe for concurrent use: the Daemon loop owns it.
type Attention struct {
	open bool
	chat string
}

// Set records the pop-out state. chat is the JID of the Chat the pop-out shows, or "" for the Chat list.
func (a *Attention) Set(open bool, chat string) { a.open, a.chat = open, chat }

// Open reports whether the pop-out is open.
func (a *Attention) Open() bool { return a.open }

// Attended returns the JID of the Attended Chat: the Chat that an open pop-out shows. It is "" when there is none.
func (a *Attention) Attended() string {
	if !a.open {
		return ""
	}
	return a.chat
}
