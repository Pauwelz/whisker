// Package chatstore holds the Chats and Messages in the encrypted chat store.
// It applies WhatsApp events to the store and lists Chats with their resolved names.
package chatstore

import (
	"database/sql"
	"strings"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB, now func() time.Time) *Store { return &Store{db: db, now: now} }

// Chat is one row of the Chat list. Name follows the name rule.
type Chat struct {
	JID        string `json:"jid"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	LastActive int64  `json:"lastActive"`
	Pinned     bool   `json:"pinned"`
	Muted      bool   `json:"muted"`
	Unread     bool   `json:"unread"`
}

// Page is a page of the Chat list. More tells whether another page follows.
type Page struct {
	Chats []Chat
	More  bool
}

// Apply stores what an event carries. It ignores events that are not about Chats.
// shortcut: App State for a Chat that History Sync has not stored yet is dropped, upgrade if the phone sends it first.
func (s *Store) Apply(ev whatsapp.Event) error {
	switch ev := ev.(type) {
	case whatsapp.HistorySync:
		return s.inTx(func(tx *sql.Tx) error {
			for _, p := range ev.PushNames {
				if err := setName(tx, "push_name", p.JID, p.Name); err != nil {
					return err
				}
			}
			for _, c := range ev.Chats {
				if err := putChat(tx, c); err != nil {
					return err
				}
			}
			return nil
		})
	case whatsapp.PushName:
		return setName(s.db, "push_name", ev.JID, ev.Name)
	case whatsapp.ContactName:
		return setName(s.db, "contact_name", ev.JID, ev.Name)
	case whatsapp.Pin:
		return s.setFlag("pinned", ev.JID, boolInt(ev.Pinned))
	case whatsapp.Mute:
		return s.setFlag("muted_until", ev.JID, ev.Until)
	case whatsapp.ReadMarker:
		return s.setFlag("unread", ev.JID, boolInt(ev.Unread))
	}
	return nil
}

// List returns Chats: Pinned first, then newest first. A Chat with no activity is not listed.
func (s *Store) List(offset, limit int) (Page, error) {
	// shortcut: offset paging can skip or repeat a Chat when the list changes between pages, upgrade to a keyset cursor if that shows.
	rows, err := s.db.Query(`
		SELECT c.jid, c.kind, c.pinned, c.unread, c.last_active,
		       c.muted_until = -1 OR c.muted_until > ?,
		       CASE WHEN c.kind = 'group' THEN c.subject
		            ELSE COALESCE(NULLIF(n.contact_name, ''), NULLIF(n.push_name, ''), '') END
		FROM chats c LEFT JOIN names n ON n.jid = c.jid
		WHERE c.last_active > 0
		ORDER BY c.pinned DESC, c.last_active DESC, c.jid
		LIMIT ? OFFSET ?`, s.now().Unix(), limit+1, offset)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Chats: []Chat{}}
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.JID, &c.Kind, &c.Pinned, &c.Unread, &c.LastActive, &c.Muted, &c.Name); err != nil {
			return Page{}, err
		}
		if c.Name == "" {
			c.Name = phoneNumber(c.JID)
		}
		page.Chats = append(page.Chats, c)
	}
	if len(page.Chats) > limit {
		page.Chats, page.More = page.Chats[:limit], true
	}
	return page, rows.Err()
}

// phoneNumber is the last step of the name rule. A JID is user[:device]@server.
func phoneNumber(jid string) string {
	user, _, _ := strings.Cut(jid, "@")
	user, _, _ = strings.Cut(user, ":")
	return "+" + user
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// setName sets one name column. column is a fixed string from this file, never user input.
func setName(db execer, column, jid, name string) error {
	_, err := db.Exec("INSERT INTO names (jid, "+column+") VALUES (?, ?) ON CONFLICT(jid) DO UPDATE SET "+column+" = excluded."+column, jid, name)
	return err
}

// setFlag sets one App State column of a stored Chat. column is a fixed string from this file.
func (s *Store) setFlag(column, jid string, value int64) error {
	_, err := s.db.Exec("UPDATE chats SET "+column+" = ? WHERE jid = ?", value, jid)
	return err
}

func putChat(tx *sql.Tx, c whatsapp.HistoryChat) error {
	last := c.LastActive
	for _, m := range c.Messages {
		last = max(last, m.Time)
	}
	_, err := tx.Exec(`
		INSERT INTO chats (jid, kind, subject, last_active, pinned, muted_until, unread) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET kind = excluded.kind,
		  subject = CASE WHEN excluded.subject = '' THEN subject ELSE excluded.subject END,
		  last_active = MAX(last_active, excluded.last_active),
		  pinned = excluded.pinned, muted_until = excluded.muted_until, unread = excluded.unread`,
		c.JID, kindOrDirect(c.Kind), c.Subject, last, boolInt(c.Pinned), c.MutedUntil, boolInt(c.Unread))
	if err != nil {
		return err
	}
	for _, m := range c.Messages {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO messages (chat_jid, id, sender_jid, from_me, time, text) VALUES (?, ?, ?, ?, ?, ?)`,
			c.JID, m.ID, m.Sender, boolInt(m.FromMe), m.Time, m.Text); err != nil {
			return err
		}
		if !m.FromMe && m.PushName != "" {
			if err := setName(tx, "push_name", m.Sender, m.PushName); err != nil {
				return err
			}
		}
	}
	return nil
}

func kindOrDirect(kind string) string {
	if kind == "" {
		return whatsapp.KindDirect
	}
	return kind
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
