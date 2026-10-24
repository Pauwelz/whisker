package chatstore

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ErrNoChat means the Chat is not in the store.
var ErrNoChat = errors.New("no such chat")

// Message is one Message of a Chat timeline, as the Plugin sees it.
// State is the Delivery State of the user's own Messages, and empty for the Messages of others.
// SenderName follows the name rule and is empty for the user's own Messages.
// Later tickets add keys here (Quote, Reactions, edit and revoke marks, Media).
type Message struct {
	ID         string `json:"id"`
	Chat       string `json:"chat"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	FromMe     bool   `json:"fromMe"`
	Time       int64  `json:"time"`
	Text       string `json:"text"`
	State      string `json:"state"`
	// Media is nil for a text Message. It never holds a path or bytes: paths come from media.open.
	Media *MediaInfo `json:"media,omitempty"`
}

// MediaInfo is the Media of a Message as the Plugin sees it. State is one of the Media states below.
type MediaInfo struct {
	Kind  media.Kind `json:"kind"`
	Mime  string     `json:"mime"`
	Name  string     `json:"name"`
	Size  int64      `json:"size"`
	State string     `json:"state"`
}

// Media states.
const (
	MediaNone        = "none"
	MediaDownloading = "downloading"
	MediaReady       = "ready"
	MediaFailed      = "failed"
)

// MessagePage is a page of a Chat timeline, oldest first. More tells whether older Messages exist.
type MessagePage struct {
	Messages []Message
	More     bool
}

// stateOrder is the order in which a Message moves. A state only moves forward.
var stateOrder = []string{whatsapp.StatePending, whatsapp.StateSent, whatsapp.StateDelivered, whatsapp.StateRead, whatsapp.StatePlayed}

// Timeline order: by time, then by arrival (rowid), so two Messages in one second keep their order.
const selectMessage = `
	SELECT m.id, m.chat_jid, m.sender_jid, m.from_me, m.time, m.text, m.state, COALESCE(n.contact_name, ''), COALESCE(n.push_name, ''),
	       COALESCE(md.kind, ''), COALESCE(md.mime, ''), COALESCE(md.name, ''), COALESCE(md.size, 0), COALESCE(md.state, '')
	FROM messages m LEFT JOIN names n ON n.jid = m.sender_jid
	LEFT JOIN media md ON md.chat_jid = m.chat_jid AND md.id = m.id `

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

// AddPending stores a new Message of the user in the Chat, in state pending. The Chat must exist.
func (s *Store) AddPending(chat, id, text string) (Message, error) {
	now := s.now().Unix()
	err := s.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE chats SET last_active = MAX(last_active, ?) WHERE jid = ?`, now, chat)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNoChat
		}
		_, err = tx.Exec(`INSERT INTO messages (chat_jid, id, sender_jid, from_me, time, text, state) VALUES (?, ?, '', 1, ?, ?, ?)`,
			chat, id, now, text, whatsapp.StatePending)
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
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	var contact, push string
	for rows.Next() {
		var m Message
		var mi MediaInfo
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.FromMe, &m.Time, &m.Text, &m.State, &contact, &push,
			&mi.Kind, &mi.Mime, &mi.Name, &mi.Size, &mi.State); err != nil {
			return nil, err
		}
		if mi.Kind != "" {
			m.Media = &mi
		}
		if !m.FromMe {
			m.SenderName = participantName(contact, push, m.Sender)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// putMessage stores a Message that arrived live or in History Sync, and the Push Name that came with it.
// mediaState is the state of the Media row: a live Message of a kind that downloads by itself starts downloading.
func putMessage(tx *sql.Tx, chat string, m whatsapp.Message, mediaState string) error {
	state := ""
	if m.FromMe {
		state = whatsapp.StateSent
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO messages (chat_jid, id, sender_jid, from_me, time, text, state) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chat, m.ID, m.Sender, boolInt(m.FromMe), m.Time, m.Text, state); err != nil {
		return err
	}
	if m.Media != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO media (chat_jid, id, kind, mime, name, size, media_key, direct_path, file_sha256, file_enc_sha256, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chat, m.ID, m.Media.Kind, m.Media.Mime, m.Media.Name, m.Media.Size, m.Media.Key, m.Media.DirectPath, m.Media.FileSHA256, m.Media.FileEncSHA256, mediaState); err != nil {
			return err
		}
	}
	if !m.FromMe && m.PushName != "" {
		return setName(tx, "push_name", m.Sender, m.PushName)
	}
	return nil
}

// MediaRef is the stored Media of a Message, with what a download or a decrypt needs.
type MediaRef struct {
	whatsapp.Media
	State string
}

// MediaOf returns the Media of a Message. It returns sql.ErrNoRows when the Message has none.
func (s *Store) MediaOf(chat, id string) (MediaRef, error) {
	var r MediaRef
	err := s.db.QueryRow(`SELECT kind, mime, name, size, media_key, direct_path, COALESCE(file_sha256, x''), COALESCE(file_enc_sha256, x''), state
		FROM media WHERE chat_jid = ? AND id = ?`, chat, id).
		Scan(&r.Kind, &r.Mime, &r.Name, &r.Size, &r.Key, &r.DirectPath, &r.FileSHA256, &r.FileEncSHA256, &r.State)
	return r, err
}

// BeginDownload moves Media from none or failed to downloading. It reports whether it moved.
func (s *Store) BeginDownload(chat, id string) (bool, error) {
	return s.change(`UPDATE media SET state = ? WHERE chat_jid = ? AND id = ? AND state IN (?, ?)`, MediaDownloading, chat, id, MediaNone, MediaFailed)
}

// EndDownload moves downloading Media to ready or failed. It reports whether it moved.
func (s *Store) EndDownload(chat, id, state string) (bool, error) {
	return s.change(`UPDATE media SET state = ? WHERE chat_jid = ? AND id = ? AND state = ?`, state, chat, id, MediaDownloading)
}

// ResetDownloads sets Media that was downloading when the Daemon stopped back to none, so a click can fetch it again.
func (s *Store) ResetDownloads() error {
	_, err := s.db.Exec(`UPDATE media SET state = ? WHERE state = ?`, MediaNone, MediaDownloading)
	return err
}

// putLiveMessage stores a Message that arrived live. It creates the Chat if History Sync has not.
func putLiveMessage(tx *sql.Tx, ev whatsapp.NewMessage) error {
	if _, err := tx.Exec(`INSERT INTO chats (jid, kind, last_active) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET last_active = MAX(last_active, excluded.last_active)`,
		ev.Chat, kindOrDirect(ev.Kind), ev.Message.Time); err != nil {
		return err
	}
	state := MediaNone
	if ev.Message.Media != nil && ev.Message.Media.Kind.Auto() {
		state = MediaDownloading
	}
	return putMessage(tx, ev.Chat, ev.Message, state)
}
