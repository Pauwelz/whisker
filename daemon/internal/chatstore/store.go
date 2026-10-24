// Package chatstore holds the Chats and Messages in the encrypted chat store.
// It applies WhatsApp events to the store and lists Chats with their resolved names.
package chatstore

import (
	"cmp"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
	// RemoveMedia deletes the stored file of a Message whose Media row was deleted. The Daemon sets it once the Media store exists.
	RemoveMedia func(chat, id string)
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
	Timer      int64  `json:"timer,omitempty"` // the Disappearing timer in seconds, absent when off
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
	case whatsapp.NewMessage:
		return s.inTx(func(tx *sql.Tx) error { return putLiveMessage(tx, ev) })
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
	case whatsapp.ChatTimer:
		return s.setFlag("timer", ev.Chat, ev.Seconds)
	}
	return nil
}

// List returns Chats: Pinned first, then newest first. A Chat with no activity is not listed.
// A non-blank query keeps the Chats whose name, or the name of a Participant who wrote in them, contains it. Case does not matter.
func (s *Store) List(offset, limit int, query string) (Page, error) {
	// shortcut: offset paging can skip or repeat a Chat when the list changes between pages, upgrade to a keyset cursor if that shows.
	// shortcut: a search reads every Chat and filters in Go, so the name rule stays in one place; upgrade if a profile holds tens of thousands of Chats.
	query = strings.ToLower(strings.TrimSpace(query))
	sqlLimit, sqlOffset := limit+1, offset
	if query != "" {
		sqlLimit, sqlOffset = -1, 0
	}
	rows, err := s.db.Query(selectChat+`
		WHERE c.last_active > 0
		ORDER BY c.pinned DESC, c.last_active DESC, c.jid
		LIMIT ? OFFSET ?`, s.now().Unix(), sqlLimit, sqlOffset)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var chats []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return Page{}, err
		}
		chats = append(chats, c)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if query != "" {
		if chats, err = s.matching(chats, query); err != nil {
			return Page{}, err
		}
		chats = chats[min(offset, len(chats)):]
	}
	page := Page{Chats: []Chat{}}
	if len(chats) > limit {
		chats, page.More = chats[:limit], true
	}
	page.Chats = append(page.Chats, chats...)
	return page, nil
}

const selectChat = `
	SELECT c.jid, c.kind, c.pinned, c.unread, c.last_active,
	       c.muted_until = -1 OR c.muted_until > ?, c.timer,
	       c.subject, COALESCE(n.contact_name, ''), COALESCE(n.push_name, '')
	FROM chats c LEFT JOIN names n ON n.jid = c.jid `

func scanChat(rows *sql.Rows) (Chat, error) {
	var c Chat
	var subject, contact, push string
	if err := rows.Scan(&c.JID, &c.Kind, &c.Pinned, &c.Unread, &c.LastActive, &c.Muted, &c.Timer, &subject, &contact, &push); err != nil {
		return Chat{}, err
	}
	c.Name = chatName(c.Kind, subject, contact, push, c.JID)
	return c, nil
}

// Chat returns one stored Chat, or sql.ErrNoRows.
func (s *Store) Chat(jid string) (Chat, error) {
	rows, err := s.db.Query(selectChat+`WHERE c.jid = ?`, s.now().Unix(), jid)
	if err != nil {
		return Chat{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Chat{}, errors.Join(sql.ErrNoRows, rows.Err())
	}
	return scanChat(rows)
}

// matching keeps the Chats that match a lower-case query by Chat name or Participant name.
func (s *Store) matching(chats []Chat, query string) ([]Chat, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT m.chat_jid, m.sender_jid, COALESCE(n.contact_name, ''), COALESCE(n.push_name, '')
		FROM messages m LEFT JOIN names n ON n.jid = m.sender_jid
		WHERE m.from_me = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	senders := map[string][]string{}
	for rows.Next() {
		var chat, jid, contact, push string
		if err := rows.Scan(&chat, &jid, &contact, &push); err != nil {
			return nil, err
		}
		senders[chat] = append(senders[chat], participantName(contact, push, jid))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	found := []Chat{}
	for _, c := range chats {
		if hasText(c.Name, query) || slices.ContainsFunc(senders[c.JID], func(n string) bool { return hasText(n, query) }) {
			found = append(found, c)
		}
	}
	return found, nil
}

func hasText(name, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(name), lowerQuery)
}

// chatName is the name rule: a Group Chat shows its subject, any other Chat the Participant name.
func chatName(kind, subject, contact, push, jid string) string {
	if kind == whatsapp.KindGroup && subject != "" {
		return subject
	}
	return participantName(contact, push, jid)
}

// ParticipantName applies the name rule to one Participant, for the names the Daemon sends outside a Message.
func (s *Store) ParticipantName(jid string) (string, error) {
	var contact, push string
	err := s.db.QueryRow(`SELECT COALESCE(contact_name, ''), COALESCE(push_name, '') FROM names WHERE jid = ?`, jid).Scan(&contact, &push)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return participantName(contact, push, jid), nil
}

// participantName is the name rule for a Participant: contact name, Push Name, then phone number.
func participantName(contact, push, jid string) string {
	return cmp.Or(contact, push, phoneNumber(jid))
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
		INSERT INTO chats (jid, kind, subject, last_active, pinned, muted_until, unread, timer) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET kind = excluded.kind,
		  subject = CASE WHEN excluded.subject = '' THEN subject ELSE excluded.subject END,
		  last_active = MAX(last_active, excluded.last_active),
		  pinned = excluded.pinned, muted_until = excluded.muted_until, unread = excluded.unread, timer = excluded.timer`,
		c.JID, kindOrDirect(c.Kind), c.Subject, last, boolInt(c.Pinned), c.MutedUntil, boolInt(c.Unread), c.Timer)
	if err != nil {
		return err
	}
	for _, m := range c.Messages {
		if err := putMessage(tx, c.JID, m, MediaNone); err != nil {
			return err
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
