package chatstore

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// ErrNoChat means the Chat is not in the store.
var ErrNoChat = errors.New("no such chat")

// Message is one Message of a Chat timeline, as the Plugin sees it.
// State is the Delivery State of the user's own Messages, and empty for the Messages of others.
// SenderName follows the name rule and is empty for the user's own Messages.
// Quote and Reactions are left out of the JSON when empty. Media is nil for a text Message.
type Message struct {
	ID         string `json:"id"`
	Chat       string `json:"chat"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	// SenderPush is true when SenderName is a Push Name: the user has no phone contact for the sender.
	SenderPush bool   `json:"senderPush,omitempty"`
	FromMe     bool   `json:"fromMe"`
	Time       int64  `json:"time"`
	Text       string `json:"text"`
	State      string `json:"state"`
	Edited     bool   `json:"edited"`
	Revoked    bool   `json:"revoked"` // a Tombstone: the text is empty
	// Media is nil for a text Message. It never holds a path or bytes: paths come from media.open.
	Media     *MediaInfo `json:"media,omitempty"`
	Quote     *Quote     `json:"quote,omitempty"`
	Reactions []Reaction `json:"reactions,omitempty"` // at most one per Participant, in the order they were first set
	// Mentions are the names in Text that the Plugin highlights, each as "@name". Text already holds them.
	Mentions []string `json:"mentions,omitempty"`
	// MentionsMe is true when the Message mentions the user. The Plugin highlights the line.
	MentionsMe bool `json:"mentionsMe,omitempty"`
	// ExpiresAt is when a Disappearing Message goes (Unix seconds). It is 0 for a Message that stays.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
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

// MediaInfo is the Media of a Message as the Plugin sees it. State is one of the Media states below.
type MediaInfo struct {
	Kind  media.Kind `json:"kind"`
	Mime  string     `json:"mime"`
	Name  string     `json:"name"`
	Size  int64      `json:"size"`
	State string     `json:"state"`
	// Thumbnail is WhatsApp's JPEG thumbnail, base64 in JSON. Absent when WhatsApp sent none.
	Thumbnail []byte `json:"thumbnail,omitempty"`
	// Duration is the length in seconds of audio, voice and video. Absent when WhatsApp sent none.
	Duration int64 `json:"duration,omitempty"`
	// Played tells that the user played the voice note in whisker.
	Played bool `json:"played,omitempty"`
	// Gif tells that a video plays as a looping GIF.
	Gif bool `json:"gif,omitempty"`
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
		       m.quote_id, m.quote_sender, m.quote_text, COALESCE(q.contact_name, ''), COALESCE(q.push_name, ''),
	       COALESCE(md.kind, ''), COALESCE(md.mime, ''), COALESCE(md.name, ''), COALESCE(md.size, 0), COALESCE(md.state, ''), m.mentions, m.expires_at, m.mentions_me, COALESCE(md.thumbnail, x''), COALESCE(md.duration, 0), COALESCE(md.played, 0), COALESCE(md.gif, 0)
	FROM messages m LEFT JOIN names n ON n.jid = m.sender_jid LEFT JOIN names q ON q.jid = m.quote_sender
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

// AddPending stores a new Message of the user in the Chat, in state pending, as a reply to quote when it is not nil.
// The Chat must exist.
func (s *Store) AddPending(chat, id, text string, quote *whatsapp.Quote) (Message, error) {
	return s.addPending(chat, id, text, quote, nil)
}

// AddPendingMedia is AddPending for a Message with Media (text is the caption). The file is in the Media store already,
// encrypted with m.Key, so the Media is ready at once and a restart or a Retry sends it again.
func (s *Store) AddPendingMedia(chat, id, text string, m *whatsapp.Media) (Message, error) {
	return s.addPending(chat, id, text, nil, m)
}

func (s *Store) addPending(chat, id, text string, quote *whatsapp.Quote, m *whatsapp.Media) (Message, error) {
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
		var timer int64
		if err := tx.QueryRow(`SELECT timer FROM chats WHERE jid = ?`, chat).Scan(&timer); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO messages (chat_jid, id, sender_jid, from_me, time, text, state, quote_id, quote_sender, quote_text, expires_at, track_recipients)
			VALUES (?, ?, '', 1, ?, ?, ?, ?, ?, ?, ?, 1)`,
			chat, id, now, text, whatsapp.StatePending, q.ID, q.Sender, q.Text, expiry(now, timer))
		if err != nil || m == nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO media (chat_jid, id, kind, mime, name, size, media_key, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			chat, id, m.Kind, m.Mime, m.Name, m.Size, m.Key, MediaReady)
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
	q, err := advanceQuery(state)
	if err != nil {
		return false, err
	}
	return s.change(q, state, chat, id)
}

// advanceQuery is the UPDATE that moves a Message of the user forward to state. Its arguments are state, chat, id.
func advanceQuery(state string) (string, error) {
	at := slices.Index(stateOrder, state)
	if at < 1 {
		return "", errors.New("not a state to advance to: " + state)
	}
	// The list holds our own constants, never input.
	before := "'" + strings.Join(stateOrder[:at], "','") + "'"
	return `UPDATE messages SET state = ? WHERE chat_jid = ? AND id = ? AND from_me = 1 AND state IN (` + before + `)`, nil
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
	changed, err := s.change(`UPDATE messages SET text = '', edited = 0, revoked = 1, mentions = '', mentions_me = 0 WHERE chat_jid = ? AND id = ? AND revoked = 0`, chat, id)
	if err != nil || !changed {
		return changed, err
	}
	if _, err := s.DeleteMedia(chat, id); err != nil {
		return true, err
	}
	return true, nil
}

// expiry is the end of a Disappearing Message that WhatsApp sent at time, or 0 for a timer of 0.
func expiry(time, timer int64) int64 {
	if timer <= 0 {
		return 0
	}
	return time + timer
}

// Expired names a Message that Expire deleted.
type Expired struct{ Chat, ID string }

// Expire deletes every Disappearing Message whose timer ended at or before now (Unix seconds), with its Media.
// A Message that expired while the Daemon was off goes at the next call. It returns the deleted Messages.
func (s *Store) Expire(now int64) ([]Expired, error) {
	rows, err := s.db.Query(`SELECT chat_jid, id FROM messages WHERE expires_at > 0 AND expires_at <= ?`, now)
	if err != nil {
		return nil, err
	}
	var gone []Expired
	for rows.Next() {
		var e Expired
		if err := rows.Scan(&e.Chat, &e.ID); err != nil {
			rows.Close()
			return nil, err
		}
		gone = append(gone, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, e := range gone {
		if err := s.DeleteMessage(e.Chat, e.ID); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// DeleteMessage deletes a Message with its Media and Reactions. Expire and "delete for me" use it.
func (s *Store) DeleteMessage(chat, id string) error {
	if _, err := s.DeleteMedia(chat, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM reactions WHERE chat_jid = ? AND message_id = ?`, chat, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE chat_jid = ? AND id = ?`, chat, id)
	return err
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
	var raw []string // the stored mentions of each Message
	var contact, push, qContact, qPush, mentions string
	for rows.Next() {
		var m Message
		var q Quote
		var mi MediaInfo
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.FromMe, &m.Time, &m.Text, &m.State, &m.Edited, &m.Revoked, &contact, &push,
			&q.ID, &q.Sender, &q.Text, &qContact, &qPush, &mi.Kind, &mi.Mime, &mi.Name, &mi.Size, &mi.State, &mentions, &m.ExpiresAt, &m.MentionsMe, &mi.Thumbnail, &mi.Duration, &mi.Played, &mi.Gif); err != nil {
			return nil, err
		}
		if mi.Kind != "" {
			m.Media = &mi
		}
		if !m.FromMe {
			m.SenderName = participantName(contact, push, m.Sender)
			m.SenderPush = contact == "" && push != ""
		}
		if q.ID != "" {
			q.FromMe = q.Sender == ""
			if !q.FromMe {
				q.SenderName = participantName(qContact, qPush, q.Sender)
			}
			m.Quote = &q
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
	q := whatsapp.Quote{}
	if m.Quote != nil {
		q = *m.Quote
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO messages (chat_jid, id, sender_jid, from_me, time, text, state, quote_id, quote_sender, quote_text, mentions, mentions_me, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chat, m.ID, m.Sender, boolInt(m.FromMe), m.Time, m.Text, state, q.ID, q.Sender, q.Text, strings.Join(m.Mentions, " "), boolInt(m.MentionsMe), expiry(m.Time, m.Timer)); err != nil {
		return err
	}
	// A stored Message moves forward to the Delivery State of History Sync.
	if m.FromMe && m.State != "" {
		q, err := advanceQuery(m.State)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(q, m.State, chat, m.ID); err != nil {
			return err
		}
	}
	if m.Media != nil {
		// A later copy, for example from another History Sync batch, only fills a thumbnail or thumbnail link that is missing.
		if _, err := tx.Exec(`INSERT INTO media (chat_jid, id, kind, mime, name, size, media_key, direct_path, file_sha256, file_enc_sha256, state, thumbnail, duration, gif,
			thumb_direct_path, thumb_sha256, thumb_enc_sha256)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (chat_jid, id) DO UPDATE SET
				thumbnail = CASE WHEN length(COALESCE(media.thumbnail, x'')) = 0 THEN excluded.thumbnail ELSE media.thumbnail END,
				thumb_sha256 = CASE WHEN media.thumb_direct_path = '' THEN excluded.thumb_sha256 ELSE media.thumb_sha256 END,
				thumb_enc_sha256 = CASE WHEN media.thumb_direct_path = '' THEN excluded.thumb_enc_sha256 ELSE media.thumb_enc_sha256 END,
				thumb_direct_path = CASE WHEN media.thumb_direct_path = '' THEN excluded.thumb_direct_path ELSE media.thumb_direct_path END`,
			chat, m.ID, m.Media.Kind, m.Media.Mime, m.Media.Name, m.Media.Size, m.Media.Key, m.Media.DirectPath, m.Media.FileSHA256, m.Media.FileEncSHA256, mediaState, m.Media.Thumbnail, m.Media.Duration, m.Media.Gif,
			m.Media.ThumbDirectPath, m.Media.ThumbSHA256, m.Media.ThumbEncSHA256); err != nil {
			return err
		}
	}
	// A Reaction that a live event already set wins over the one History Sync carries.
	for _, r := range m.Reactions {
		sender := r.Sender
		if r.FromMe {
			sender = ""
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO reactions (chat_jid, message_id, sender_jid, emoji) VALUES (?, ?, ?, ?)`,
			chat, m.ID, sender, r.Emoji); err != nil {
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
	err := s.db.QueryRow(`SELECT kind, mime, name, size, media_key, direct_path, COALESCE(file_sha256, x''), COALESCE(file_enc_sha256, x''), state,
		COALESCE(thumbnail, x''), thumb_direct_path, COALESCE(thumb_sha256, x''), COALESCE(thumb_enc_sha256, x'')
		FROM media WHERE chat_jid = ? AND id = ?`, chat, id).
		Scan(&r.Kind, &r.Mime, &r.Name, &r.Size, &r.Key, &r.DirectPath, &r.FileSHA256, &r.FileEncSHA256, &r.State,
			&r.Thumbnail, &r.ThumbDirectPath, &r.ThumbSHA256, &r.ThumbEncSHA256)
	return r, err
}

// SetThumbnail stores the thumbnail of Media that came without one. It reports whether the Media still lacked it.
func (s *Store) SetThumbnail(chat, id string, data []byte) (bool, error) {
	return s.change(`UPDATE media SET thumbnail = ? WHERE chat_jid = ? AND id = ? AND length(COALESCE(thumbnail, x'')) = 0`, data, chat, id)
}

// SetPlayed marks a voice note as played by the user. It reports whether the mark is new.
func (s *Store) SetPlayed(chat, id string) (bool, error) {
	return s.change(`UPDATE media SET played = 1 WHERE chat_jid = ? AND id = ? AND played = 0`, chat, id)
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

// LivePolicy is what the caller decides about a live Message. The store writes it as told.
type LivePolicy struct {
	MediaState string // the state of its Media row: MediaNone, or MediaDownloading for Media that downloads by itself
	MarkUnread bool   // set the Read Marker of the Chat and keep the Message waiting for its Read Receipt
}

// PutLive stores a Message that arrived live. It creates the Chat if History Sync has not.
// A Message that is stored already changes nothing, also not the Read Marker.
func (s *Store) PutLive(ev whatsapp.NewMessage, p LivePolicy) error {
	return s.inTx(func(tx *sql.Tx) error { return putLiveMessage(tx, ev, p) })
}

func putLiveMessage(tx *sql.Tx, ev whatsapp.NewMessage, p LivePolicy) error {
	// A Message that knows its kind fixes the kind of a row that a Pin stored.
	if _, err := tx.Exec(`INSERT INTO chats (jid, kind, last_active) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET kind = COALESCE(NULLIF(?, ''), kind), last_active = MAX(last_active, excluded.last_active)`,
		ev.Chat, kindOrDirect(ev.Kind), ev.Message.Time, ev.Kind); err != nil {
		return err
	}
	var known int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND id = ?`, ev.Chat, ev.Message.ID).Scan(&known); err != nil {
		return err
	}
	if err := putMessage(tx, ev.Chat, ev.Message, p.MediaState); err != nil {
		return err
	}
	if known > 0 {
		return nil
	}
	if ev.Message.FromMe { // History Sync Messages never get this: see Combined
		if _, err := tx.Exec(`UPDATE messages SET track_recipients = 1 WHERE chat_jid = ? AND id = ?`, ev.Chat, ev.Message.ID); err != nil {
			return err
		}
	}
	// With "Keep chats archived" off, a new Message unarchives its Chat. The live bit stops a later History Sync from archiving it again.
	if _, err := tx.Exec(`UPDATE chats SET archived = 0, live_flags = live_flags | ? WHERE jid = ? AND archived = 1
		AND (SELECT value FROM app_state_settings WHERE name = ?) = 1`, flagArchive, ev.Chat, settingUnarchiveChats); err != nil {
		return err
	}
	if !p.MarkUnread {
		return nil
	}
	if _, err := tx.Exec(`UPDATE messages SET read_pending = 1 WHERE chat_jid = ? AND id = ?`, ev.Chat, ev.Message.ID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE chats SET unread = 1, live_flags = live_flags | ? WHERE jid = ?`, flagUnread, ev.Chat)
	return err
}

// ClearUnread clears the Read Marker of a Chat. It reports whether the marker was set.
func (s *Store) ClearUnread(chat string) (bool, error) {
	return s.change(`UPDATE chats SET unread = 0, live_flags = live_flags | ? WHERE jid = ? AND unread = 1`, flagUnread, chat)
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
