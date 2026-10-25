package chatstore

import (
	"regexp"
	"slices"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// Contact is one person the user can start a Chat with. Phone is empty for a LID.
// PushNameOnly is true when the phone holds no contact name. Chat is the WhatsApp ID of the stored Chat with the person (also an empty one that was started), or empty.
type Contact struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	PushNameOnly bool   `json:"pushNameOnly"`
	Chat         string `json:"chat"`
}

// personJID is the WhatsApp ID of one person: a phone or a LID, without a device part.
var personJID = regexp.MustCompile(`^[0-9]+@(s\.whatsapp\.net|lid)$`)

// IsPerson tells that jid is a person's WhatsApp ID in phone or LID form.
func IsPerson(jid string) bool { return personJID.MatchString(jid) }

// Contacts lists the names the store holds: Phone Contacts by name, then Participants known only by a Push Name.
// A non-blank query keeps those whose name contains it (case does not matter) or whose phone number holds its digits.
// own are the user parts of the user's own IDs; they are never listed. At most limit people come back.
// shortcut: it reads every name and filters in Go, like List; upgrade if a profile holds tens of thousands of names.
func (s *Store) Contacts(query string, limit int, own []string) ([]Contact, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	digits := whatsapp.Digits(query)
	rows, err := s.db.Query(`SELECT n.jid, n.contact_name, n.push_name,
		COALESCE((SELECT c.jid FROM chats c WHERE c.jid = n.jid), '')
		FROM names n WHERE n.contact_name <> '' OR n.push_name <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []Contact{}
	for rows.Next() {
		var c Contact
		var contact, push string
		if err := rows.Scan(&c.JID, &contact, &push, &c.Chat); err != nil {
			return nil, err
		}
		user, _, _ := strings.Cut(c.JID, "@")
		if !IsPerson(c.JID) || slices.Contains(own, user) {
			continue
		}
		c.PushNameOnly = contact == ""
		c.Name = contact
		if c.PushNameOnly {
			c.Name = push
		}
		if strings.HasSuffix(c.JID, "@s.whatsapp.net") {
			c.Phone = phoneNumber(c.JID)
		}
		if query != "" && !hasText(c.Name, query) && (digits == "" || !strings.Contains(whatsapp.Digits(c.Phone), digits)) {
			continue
		}
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b Contact) int {
		if a.PushNameOnly != b.PushNameOnly {
			if a.PushNameOnly {
				return 1
			}
			return -1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.JID, b.JID)
	})
	return found[:min(limit, len(found))], nil
}

// StartChat returns the stored Chat of a person, or stores a new empty one. The caller checks IsPerson.
// An empty Chat is not listed until it has a Message or a Draft.
func (s *Store) StartChat(jid string) (Chat, error) {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO chats (jid) VALUES (?)`, jid); err != nil {
		return Chat{}, err
	}
	return s.Chat(jid)
}
