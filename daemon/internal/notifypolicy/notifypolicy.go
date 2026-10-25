// Package notifypolicy decides whether a new Message raises a desktop notification. It is pure logic.
package notifypolicy

// Input is what the policy needs to know about one new Message.
type Input struct {
	FromMe        bool // the user sent it from another device
	Muted         bool // its Chat is Muted
	Archived      bool // its Chat stays archived after the Message ("Keep chats archived")
	Mentioned     bool // it mentions the user: a Muted or archived Chat still notifies, as in WhatsApp
	Attended      bool // its Chat is the Attended Chat
	Notifications bool // the notifications setting
}

// Notifies reports whether the Message raises a notification.
func Notifies(in Input) bool {
	return in.Notifications && !in.FromMe && (!(in.Muted || in.Archived) || in.Mentioned) && !in.Attended
}
