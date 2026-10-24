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
// Later tickets add keys here (Quote, Reactions, Media).
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
	Media   *MediaInfo `json:"media,omitempty"`
	Edited  bool       `json:"edited"`
	Revoked bool       `json:"revoked"` // a Tombstone: the text is empty
	// Mentions are the names in Text that the Plugin highlights, each as "@name". Text already holds them.
	Mentions []string `json:"mentions,omitempty"`
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
	SELECT m.id, m.chat_jid, m.sender_jid, m.from_me, m.time, m.text, m.state, m.edited, m.revoked, COALESCE(n.contact_name, ''), COALESCE(n.push_name, ''),
	       COALESCE(md.kind, ''), COALESCE(md.mime, ''), COALESCE(md.name, ''), COALESCE(md.size, 0), COALESCE(md.state, ''), m.mentions
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

// Edit replaces the text of a Message that is not a Tombstone and marks it edited.
// Only the author can edit: fromMe and sender must match the stored Message. It reports whether the Message changed.
func (s *Store) Edit(chat, id, sender string, fromMe bool, text string) (bool, error) {
	return s.change(`UPDATE messages SET text = ?, edited = 1 WHERE chat_jid = ? AND id = ? AND revoked = 0 AND from_me = ? AND (from_me = 1 OR sender_jid = ?)`,
		text, chat, id, boolInt(fromMe), sender)
}

// Revoke turns a Message into a Tombstone: it empties the text and keeps the position. It reports whether the Message changed.
// A Tombstone keeps no Media: the Media row goes, and so does the stored file (through RemoveMedia).
func (s *Store) Revoke(chat, id string) (bool, error) {
	changed, err := s.change(`UPDATE messages SET text = '', edited = 0, revoked = 1, mentions = '' WHERE chat_jid = ? AND id = ? AND revoked = 0`, chat, id)
	if err != nil || !changed {
		return changed, err
	}
	if _, err := s.DeleteMedia(chat, id); err != nil {
		return true, err
	}
	return true, nil
}

// DeleteMedia removes the Media row of a Message and calls RemoveMedia. It reports whether a row existed.
// Disappearing Messages use it too.
func (s *Store) DeleteMedia(chat, id string) (bool, error) {
	had, err := s.change(`DELETE FROM media WHERE chat_jid = ? AND id = ?`, chat, id)
	if had && s.RemoveMedia != nil {
		s.RemoveMedia(chat, id)
	}
	return had, err
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
	var raw []string // the stored mentions of each Message
	var contact, push, mentions string
	for rows.Next() {
		var m Message
		var mi MediaInfo
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.FromMe, &m.Time, &m.Text, &m.State, &m.Edited, &m.Revoked, &contact, &push,
			&mi.Kind, &mi.Mime, &mi.Name, &mi.Size, &mi.State, &mentions); err != nil {
			return nil, err
		}
		if mi.Kind != "" {
			m.Media = &mi
		}
		if !m.FromMe {
			m.SenderName = participantName(contact, push, m.Sender)
		}
		msgs = append(msgs, m)
		raw = append(raw, mentions)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i, jids := range raw {
		if err := s.nameMentions(&msgs[i], jids); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// nameMentions replaces "@<user part>" in the text with "@<name>" (the name rule) and lists the names.
// shortcut: a plain replace, so "@12" inside "@123" is also replaced; upgrade if mentions of near numbers appear.
func (s *Store) nameMentions(m *Message, jids string) error {
	for _, jid := range strings.Fields(jids) {
		name, err := s.ParticipantName(jid)
		if err != nil {
			return err
		}
		user, _, _ := strings.Cut(jid, "@")
		label := "@" + name
		m.Text = strings.ReplaceAll(m.Text, "@"+user, label)
		m.Mentions = append(m.Mentions, label)
	}
	return nil
}

// putMessage stores a Message that arrived live or in History Sync, and the Push Name that came with it.
// mediaState is the state of the Media row: a live Message of a kind that downloads by itself starts downloading.
func putMessage(tx *sql.Tx, chat string, m whatsapp.Message, mediaState string) error {
	state := ""
	if m.FromMe {
		state = whatsapp.StateSent
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO messages (chat_jid, id, sender_jid, from_me, time, text, state, mentions) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		chat, m.ID, m.Sender, boolInt(m.FromMe), m.Time, m.Text, state, strings.Join(m.Mentions, " ")); err != nil {
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
	mediaState := MediaNone
	if ev.Message.Media != nil && ev.Message.Media.Kind.Auto() {
		mediaState = MediaDownloading
	}
	if err := putMessage(tx, ev.Chat, ev.Message, mediaState); err != nil {
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
