package chatstore

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ErrNoChat means the Chat is not in the store.
var ErrNoChat = errors.New("no such chat")

// Message is one Message of a Chat timeline, as the Plugin sees it.
// State is the Delivery State of the user's own Messages, and empty for the Messages of others.
// SenderName follows the name rule and is empty for the user's own Messages.
// Quote and Reactions are left out of the JSON when empty. Later tickets add keys here (Media).
type Message struct {
	ID         string     `json:"id"`
	Chat       string     `json:"chat"`
	Sender     string     `json:"sender"`
	SenderName string     `json:"senderName"`
	FromMe     bool       `json:"fromMe"`
	Time       int64      `json:"time"`
	Text       string     `json:"text"`
	State      string     `json:"state"`
	Edited     bool       `json:"edited"`
	Revoked    bool       `json:"revoked"` // a Tombstone: the text is empty
	Quote      *Quote     `json:"quote,omitempty"`
	Reactions  []Reaction `json:"reactions,omitempty"` // at most one per Participant, in the order they were first set
}

// Quote is the Message that a reply quotes: a snapshot taken when the reply was stored.
// Sender is the JID of its author, empty for the user. SenderName follows the name rule and is empty for the user.
type Quote struct {
	ID         string `json:"id"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	FromMe     bool   `json:"fromMe"`
	Text       string `json:"text"`
}

// Reaction is the emoji of one Participant on a Message. Sender is empty for the user.
type Reaction struct {
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	FromMe     bool   `json:"fromMe"`
	Emoji      string `json:"emoji"`
}

// MessagePage is a page of a Chat timeline, oldest first. More tells whether older Messages exist.
type MessagePage struct {
	Messages []Message
	More     bool
}

// stateOrder is the order in which a Message moves. A state only moves forward.
var stateOrder = []string{whatsapp.StatePending, whatsapp.StateSent, whatsapp.StateDelivered, whatsapp.StateRead, whatsapp.StatePlayed}

// Timeline order: by time, then by arrival (rowid), so two Messages in one second keep their order.
const selectMessage = `
	SELECT m.id, m.chat_jid, m.sender_jid, m.from_me, m.time, m.text, m.state, m.edited, m.revoked, COALESCE(n.contact_name, ''), COALESCE(n.push_name, ''),
		       m.quote_id, m.quote_sender, m.quote_text, COALESCE(q.contact_name, ''), COALESCE(q.push_name, '')
	FROM messages m LEFT JOIN names n ON n.jid = m.sender_jid LEFT JOIN names q ON q.jid = m.quote_sender `

// Messages returns one page of a Chat timeline. Offset 0 is the newest page.
func (s *Store) Messages(chat string, offset, limit int) (MessagePage, error) {
	// shortcut: offset paging can repeat a Message when a new one arrives between pages, the Plugin drops repeats by id.
	msgs, err := s.query(selectMessage+`WHERE m.chat_jid = ? ORDER BY m.time DESC, m.rowid DESC LIMIT ? OFFSET ?`, chat, limit+1, offset)
	if err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{More: len(msgs) > limit}
	if page.More {
		msgs = msgs[:limit]
	}
	for i := len(msgs) - 1; i >= 0; i-- { // newest first in the query, oldest first in the page
		page.Messages = append(page.Messages, msgs[i])
	}
	if page.Messages == nil {
		page.Messages = []Message{}
	}
	return page, nil
}

// Message returns one stored Message.
func (s *Store) Message(chat, id string) (Message, error) {
	msgs, err := s.query(selectMessage+`WHERE m.chat_jid = ? AND m.id = ?`, chat, id)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return msgs[0], nil
}

// Pending returns the Messages that wait to be sent, oldest first.
func (s *Store) Pending() ([]Message, error) {
	return s.query(selectMessage+`WHERE m.state = ? ORDER BY m.time, m.rowid`, whatsapp.StatePending)
}

// AddPending stores a new Message of the user in the Chat, in state pending, as a reply to quote when it is not nil.
// The Chat must exist.
func (s *Store) AddPending(chat, id, text string, quote *whatsapp.Quote) (Message, error) {
	q := whatsapp.Quote{}
	if quote != nil {
		q = *quote
	}
	now := s.now().Unix()
	err := s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE chats SET last_active = MAX(last_active, ?) WHERE jid = ?`, now, chat)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNoChat
		}
		_, err = tx.Exec(`INSERT INTO messages (chat_jid, id, sender_jid, from_me, time, text, state, quote_id, quote_sender, quote_text)
			VALUES (?, ?, '', 1, ?, ?, ?, ?, ?, ?)`,
			chat, id, now, text, whatsapp.StatePending, q.ID, q.Sender, q.Text)
		return err
	})
	if err != nil {
		return Message{}, err
	}
	return s.Message(chat, id)
}

// Advance moves a Message of the user forward to state (delivered, read or played, also sent).
// It reports whether the Message changed: a Message never moves back, and a failed Message does not move.
func (s *Store) Advance(chat, id, state string) (bool, error) {
	at := -1
	for i, st := range stateOrder {
		if st == state {
			at = i
		}
	}
	if at < 1 {
		return false, errors.New("not a state to advance to: " + state)
	}
	// The list holds our own constants, never input.
	before := "'" + strings.Join(stateOrder[:at], "','") + "'"
	return s.change(`UPDATE messages SET state = ? WHERE chat_jid = ? AND id = ? AND from_me = 1 AND state IN (`+before+`)`, state, chat, id)
}

// Fail marks a pending Message as failed. Requeue makes a failed Message pending again (Retry).
func (s *Store) Fail(chat, id string) (bool, error) {
	return s.move(chat, id, whatsapp.StatePending, whatsapp.StateFailed)
}

func (s *Store) Requeue(chat, id string) (bool, error) {
	return s.move(chat, id, whatsapp.StateFailed, whatsapp.StatePending)
}

// Edit replaces the text of a Message that is not a Tombstone and marks it edited.
// Only the author can edit: fromMe and sender must match the stored Message. It reports whether the Message changed.
func (s *Store) Edit(chat, id, sender string, fromMe bool, text string) (bool, error) {
	return s.change(`UPDATE messages SET text = ?, edited = 1 WHERE chat_jid = ? AND id = ? AND revoked = 0 AND from_me = ? AND (from_me = 1 OR sender_jid = ?)`,
		text, chat, id, boolInt(fromMe), sender)
}

// Revoke turns a Message into a Tombstone: it empties the text and keeps the position. It reports whether the Message changed.
func (s *Store) Revoke(chat, id string) (bool, error) {
	return s.change(`UPDATE messages SET text = '', edited = 0, revoked = 1 WHERE chat_jid = ? AND id = ? AND revoked = 0`, chat, id)
}

// SetReaction sets the Reaction of a Participant on a stored Message, or removes it when emoji is empty.
// The reactor is the user when fromMe is true. It reports whether anything changed: an unknown Message or the same emoji changes nothing.
func (s *Store) SetReaction(chat, id, sender string, fromMe bool, emoji string) (bool, error) {
	if fromMe {
		sender = ""
	}
	if emoji == "" {
		return s.change(`DELETE FROM reactions WHERE chat_jid = ? AND message_id = ? AND sender_jid = ?`, chat, id, sender)
	}
	return s.change(`INSERT INTO reactions (chat_jid, message_id, sender_jid, emoji)
		SELECT ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM messages WHERE chat_jid = ? AND id = ?)
		ON CONFLICT (chat_jid, message_id, sender_jid) DO UPDATE SET emoji = excluded.emoji WHERE emoji != excluded.emoji`,
		chat, id, sender, emoji, chat, id)
}

func (s *Store) move(chat, id, from, to string) (bool, error) {
	return s.change(`UPDATE messages SET state = ? WHERE chat_jid = ? AND id = ? AND state = ?`, to, chat, id, from)
}

func (s *Store) change(query string, args ...any) (bool, error) {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) query(query string, args ...any) ([]Message, error) {
	msgs, err := s.queryRows(query, args...)
	if err != nil {
		return nil, err
	}
	// shortcut: one query per Message for its Reactions, upgrade to one query per page if a page gets slow.
	for i := range msgs {
		if msgs[i].Reactions, err = s.reactions(msgs[i].Chat, msgs[i].ID); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// reactions returns the Reactions of a Message in the order they were first set. It is nil for none.
func (s *Store) reactions(chat, id string) ([]Reaction, error) {
	rows, err := s.db.Query(`
		SELECT r.sender_jid, r.emoji, COALESCE(n.contact_name, ''), COALESCE(n.push_name, '')
		FROM reactions r LEFT JOIN names n ON n.jid = r.sender_jid
		WHERE r.chat_jid = ? AND r.message_id = ? ORDER BY r.rowid`, chat, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Reaction
	for rows.Next() {
		var r Reaction
		var contact, push string
		if err := rows.Scan(&r.Sender, &r.Emoji, &contact, &push); err != nil {
			return nil, err
		}
		if r.Sender == "" {
			r.FromMe = true
		} else {
			r.SenderName = participantName(contact, push, r.Sender)
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

func (s *Store) queryRows(query string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	var contact, push, qContact, qPush string
	for rows.Next() {
		var m Message
		var q Quote
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.FromMe, &m.Time, &m.Text, &m.State, &m.Edited, &m.Revoked, &contact, &push,
			&q.ID, &q.Sender, &q.Text, &qContact, &qPush); err != nil {
			return nil, err
		}
		if !m.FromMe {
			m.SenderName = participantName(contact, push, m.Sender)
		}
		if q.ID != "" {
			q.FromMe = q.Sender == ""
			if !q.FromMe {
				q.SenderName = participantName(qContact, qPush, q.Sender)
			}
			m.Quote = &q
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// putMessage stores a Message that arrived live or in History Sync, and the Push Name that came with it.
func putMessage(tx *sql.Tx, chat string, m whatsapp.Message) error {
	state := ""
	if m.FromMe {
		state = whatsapp.StateSent
	}
	q := whatsapp.Quote{}
	if m.Quote != nil {
		q = *m.Quote
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO messages (chat_jid, id, sender_jid, from_me, time, text, state, quote_id, quote_sender, quote_text)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chat, m.ID, m.Sender, boolInt(m.FromMe), m.Time, m.Text, state, q.ID, q.Sender, q.Text); err != nil {
		return err
	}
	if !m.FromMe && m.PushName != "" {
		return setName(tx, "push_name", m.Sender, m.PushName)
	}
	return nil
}

// putLiveMessage stores a Message that arrived live. It creates the Chat if History Sync has not.
// A new Message of another Participant sets the Read Marker and waits for its Read Receipt.
func putLiveMessage(tx *sql.Tx, ev whatsapp.NewMessage) error {
	if _, err := tx.Exec(`INSERT INTO chats (jid, kind, last_active) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET last_active = MAX(last_active, excluded.last_active)`,
		ev.Chat, kindOrDirect(ev.Kind), ev.Message.Time); err != nil {
		return err
	}
	var known int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND id = ?`, ev.Chat, ev.Message.ID).Scan(&known); err != nil {
		return err
	}
	if err := putMessage(tx, ev.Chat, ev.Message); err != nil {
		return err
	}
	if known > 0 || ev.Message.FromMe {
		return nil
	}
	if _, err := tx.Exec(`UPDATE messages SET read_pending = 1 WHERE chat_jid = ? AND id = ?`, ev.Chat, ev.Message.ID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE chats SET unread = 1 WHERE jid = ?`, ev.Chat)
	return err
}

// ClearUnread clears the Read Marker of a Chat. It reports whether the marker was set.
func (s *Store) ClearUnread(chat string) (bool, error) {
	return s.change(`UPDATE chats SET unread = 0 WHERE jid = ? AND unread = 1`, chat)
}

// Unreceipted is a Message of another Participant that waits for its Read Receipt.
type Unreceipted struct{ ID, Sender string }

// Unreceipted returns the Messages of a Chat that wait for a Read Receipt, oldest first.
func (s *Store) Unreceipted(chat string) ([]Unreceipted, error) {
	rows, err := s.db.Query(`SELECT id, sender_jid FROM messages WHERE chat_jid = ? AND read_pending = 1 ORDER BY time, rowid`, chat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Unreceipted
	for rows.Next() {
		var u Unreceipted
		if err := rows.Scan(&u.ID, &u.Sender); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// Receipted records that the Read Receipt of these Messages is sent.
func (s *Store) Receipted(chat string, ids []string) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`UPDATE messages SET read_pending = 0 WHERE chat_jid = ? AND id = ?`, chat, id); err != nil {
				return err
			}
		}
		return nil
	})
}
