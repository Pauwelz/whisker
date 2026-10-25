package chatstore

import (
	"cmp"
	"database/sql"
	"strconv"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// PutSystem stores a live System Message. It keeps the Chat quiet: no Read Marker, no new last_active, no unarchive.
// It creates the Chat if History Sync has not. A new subject also renames the Group Chat.
func (s *Store) PutSystem(ev whatsapp.NewMessage) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO chats (jid, kind) VALUES (?, ?) ON CONFLICT(jid) DO UPDATE SET kind = COALESCE(NULLIF(?, ''), kind)`,
			ev.Chat, kindOrDirect(ev.Kind), ev.Kind); err != nil {
			return err
		}
		if sys := ev.Message.System; sys.Kind == whatsapp.SystemSubject {
			// Only the live path renames: a History Sync stub can be older than the subject of its Chat.
			if _, err := tx.Exec(`UPDATE chats SET subject = ? WHERE jid = ?`, sys.Value, ev.Chat); err != nil {
				return err
			}
		}
		_, err := putMessage(tx, ev.Chat, ev.Message, MediaNone)
		return err
	})
}

// systemActor is the Participant who made the change of a System Message: the user, or a name (the name rule).
type systemActor struct {
	Me   bool
	Name string
}

// systemText is the line of a System Message. targets hold names (the name rule), and "" for the user.
// The user is "You" at the start of the line and "you" in other places.
func systemText(kind string, actor systemActor, targets []string, value string) string {
	who := cmp.Or(actor.Name, "Someone")
	if actor.Me {
		who = "You"
	}
	admins := "an admin"
	if len(targets) > 1 {
		admins = "admins"
	}
	switch kind {
	case whatsapp.SystemAdd:
		return who + " added " + nameList(targets, false)
	case whatsapp.SystemInvite:
		return nameList(targets, true) + " joined using an invite link"
	case whatsapp.SystemLeave:
		return nameList(targets, true) + " left"
	case whatsapp.SystemRemove:
		return who + " removed " + nameList(targets, false)
	case whatsapp.SystemPromote:
		return who + " made " + nameList(targets, false) + " " + admins
	case whatsapp.SystemDemote:
		return who + " removed " + nameList(targets, false) + " as admin"
	case whatsapp.SystemSubject:
		return who + ` changed the subject to "` + value + `"`
	case whatsapp.SystemDescription:
		if value == whatsapp.SystemDeleted {
			return who + " deleted the group description"
		}
		return who + " changed the group description"
	case whatsapp.SystemIcon:
		if value == whatsapp.SystemDeleted {
			return who + " removed the group icon"
		}
		return who + " changed the group icon"
	case whatsapp.SystemCreate:
		return who + " created the group"
	case whatsapp.SystemTimer:
		seconds, _ := strconv.ParseInt(value, 10, 64)
		if seconds <= 0 {
			return who + " turned off disappearing messages"
		}
		return who + " set the disappearing timer to " + timerLabel(seconds)
	}
	return ""
}

// nameList joins names as "A, B and C". The user ("") is "You" when the list starts the line, else "you".
func nameList(names []string, start bool) string {
	out := make([]string, len(names))
	for i, n := range names {
		switch {
		case n != "":
			out[i] = n
		case i == 0 && start:
			out[i] = "You"
		default:
			out[i] = "you"
		}
	}
	if len(out) < 2 {
		return strings.Join(out, "")
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

// timerLabel names a Disappearing timer as WhatsApp does: "24 hours", "7 days", "90 days".
func timerLabel(seconds int64) string {
	const day, hour = 86400, 3600
	switch {
	case seconds > day && seconds%day == 0:
		return strconv.FormatInt(seconds/day, 10) + " days"
	case seconds%hour == 0:
		return plural(seconds/hour, "hour")
	}
	return plural(seconds, "second")
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.FormatInt(n, 10) + " " + unit + "s"
}
