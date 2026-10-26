// Package chatstore holds the Chats and Messages in the encrypted chat store.
// It applies WhatsApp events to the store and lists Chats with their resolved names.
package chatstore

import (
	"cmp"
	"database/sql"
	"errors"
	"slices"
	"strconv"
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
	Archived   bool   `json:"archived,omitempty"`
	Muted      bool   `json:"muted"`
	// MutedUntil is the end of the Mute in Unix seconds, -1 for ever. It is absent when the Chat is not Muted.
	MutedUntil int64 `json:"mutedUntil,omitempty"`
	Unread     bool  `json:"unread"`
	// UnreadCount is the number of live Messages that wait for a Read Receipt. It is 0 for a read Chat.
	// A Chat that the phone marked unread has none, so the Plugin shows a dot.
	UnreadCount int `json:"unreadCount,omitempty"`
	// Mention is true when one of those Messages mentions the user.
	Mention bool  `json:"mention,omitempty"`
	Timer   int64 `json:"timer,omitempty"` // the Disappearing timer in seconds, absent when off
}

// Page is a page of the Chat list. More tells whether another page follows.
type Page struct {
	Chats []Chat
	More  bool
}

// Apply stores what an event of History Sync or App State carries. It reports false for any other event, which it ignores.
// For a ReadElsewhere it reports false when the Chat list did not change.
// shortcut: App State for a Chat that History Sync has not stored yet is dropped, upgrade if the phone sends it first.
func (s *Store) Apply(ev whatsapp.Event) (stored bool, err error) {
	switch ev := ev.(type) {
	case whatsapp.HistorySync:
		_, err = s.ApplyHistory(ev)
	case whatsapp.PushName:
		err = setName(s.db, "push_name", ev.JID, ev.Name)
	case whatsapp.ContactName:
		err = setName(s.db, "contact_name", ev.JID, ev.Name)
	case whatsapp.ContactBook:
		err = s.inTx(func(tx *sql.Tx) error {
			for _, c := range ev.Names {
				if err := setName(tx, "contact_name", c.JID, c.Name); err != nil {
					return err
				}
			}
			return nil
		})
	case whatsapp.Pin, whatsapp.Archive, whatsapp.UnarchiveChats, whatsapp.Mute:
		err = applyFlag(s.db, ev)
	case whatsapp.ReadMarker: // It writes the Messages and the Chat, so it needs one transaction.
		err = s.inTx(func(tx *sql.Tx) error { return applyFlag(tx, ev) })
	case whatsapp.AppStateBatch:
		err = s.inTx(func(tx *sql.Tx) error {
			for _, e := range ev.Events {
				if err := applyFlag(tx, e); err != nil {
					return err
				}
			}
			return nil
		})
	case whatsapp.ReadElsewhere:
		err = s.inTx(func(tx *sql.Tx) error {
			var e error
			stored, e = readElsewhere(tx, ev)
			return e
		})
		return stored, err
	case whatsapp.ChatTimer:
		err = setFlag(s.db, "timer", flagTimer, ev.Chat, ev.Seconds)
	default:
		return false, nil
	}
	return true, err
}

// ApplyHistory stores a History Sync batch. It returns the number of Messages that were new, per Chat of the batch.
func (s *Store) ApplyHistory(ev whatsapp.HistorySync) (added map[string]int, err error) {
	added = map[string]int{}
	err = s.inTx(func(tx *sql.Tx) error {
		for _, p := range ev.PushNames {
			if err := setName(tx, "push_name", p.JID, p.Name); err != nil {
				return err
			}
		}
		for _, c := range ev.Chats {
			n, err := putChat(tx, c)
			if err != nil {
				return err
			}
			added[c.JID] += n
		}
		return nil
	})
	return added, err
}

// OldestMessage returns the oldest stored Message of a Chat, or sql.ErrNoRows. It skips System Messages: their ID can be one the Daemon made.
func (s *Store) OldestMessage(chat string) (a whatsapp.HistoryAnchor, err error) {
	err = s.db.QueryRow(`SELECT id, from_me, time FROM messages WHERE chat_jid = ? AND system_kind = '' ORDER BY time, rowid LIMIT 1`, chat).Scan(&a.ID, &a.FromMe, &a.Time)
	return a, err
}

// List returns Chats: Pinned first, then newest first. A Chat with no Message and no Draft is not listed.
// Without a query it lists the archived Chats if archived is true, else the others.
// A non-blank query keeps the Chats whose name, or the name of a Participant who wrote in them, contains it. Case does not matter.
// A query searches the archived Chats too, unless archived is true: then it searches only them.
func (s *Store) List(offset, limit int, query string, archived bool) (Page, error) {
	// shortcut: offset paging can skip or repeat a Chat when the list changes between pages, upgrade to a keyset cursor if that shows.
	// shortcut: a search reads every Chat and filters in Go, so the name rule stays in one place; upgrade if a profile holds tens of thousands of Chats.
	query = strings.ToLower(strings.TrimSpace(query))
	sqlLimit, sqlOffset := limit+1, offset
	where := listed + ` AND c.archived = ` + strconv.Itoa(int(boolInt(archived)))
	if query != "" {
		sqlLimit, sqlOffset = -1, 0
		if !archived {
			where = listed
		}
	}
	rows, err := s.db.Query(selectChat+`
		WHERE `+where+`
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

// UnreadChats counts the unread Chats that are not Muted or archived, over the whole chat store. The bar shows it.
// It counts the Chats that List shows, so the bar and the list agree.
func (s *Store) UnreadChats() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chats c WHERE `+listed+` AND c.unread AND NOT c.archived AND NOT `+mutedNow, s.now().Unix()).Scan(&n)
	return n, err
}

// NextMuteEnd returns the earliest future end of a Mute, in Unix seconds. It reports false when no Mute will end.
func (s *Store) NextMuteEnd() (until int64, ok bool, err error) {
	var end sql.NullInt64
	err = s.db.QueryRow(`SELECT MIN(c.muted_until) FROM chats c WHERE c.muted_until > ?`, s.now().Unix()).Scan(&end)
	return end.Int64, end.Valid, err
}

// PinnedCount counts the Pinned Chats of the whole chat store, listed or not. WhatsApp counts them all.
func (s *Store) PinnedCount() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM chats WHERE pinned = 1`).Scan(&n)
	return n, err
}

// listed is the SQL condition of a Chat that List shows. mutedNow is the condition of a Muted Chat, with one time parameter.
const (
	listed   = `(c.last_active > 0 OR EXISTS (SELECT 1 FROM drafts d WHERE d.chat_jid = c.jid))`
	mutedNow = `(c.muted_until = -1 OR c.muted_until > ?)`
)

const selectChat = `
	SELECT c.jid, c.kind, c.pinned, c.archived, c.unread, c.last_active,
	       CASE WHEN ` + mutedNow + ` THEN c.muted_until ELSE 0 END, c.timer,
	       CASE WHEN c.unread THEN (SELECT COUNT(*) FROM messages m WHERE m.chat_jid = c.jid AND m.in_unread_count = 1) ELSE 0 END,
	       c.unread AND EXISTS (SELECT 1 FROM messages m WHERE m.chat_jid = c.jid AND m.in_unread_count = 1 AND m.mentions_me),
	       c.subject, COALESCE(n.contact_name, ''), COALESCE(n.push_name, '')
	FROM chats c LEFT JOIN names n ON n.jid = c.jid `

func scanChat(rows *sql.Rows) (Chat, error) {
	var c Chat
	var subject, contact, push string
	if err := rows.Scan(&c.JID, &c.Kind, &c.Pinned, &c.Archived, &c.Unread, &c.LastActive, &c.MutedUntil, &c.Timer, &c.UnreadCount, &c.Mention, &subject, &contact, &push); err != nil {
		return Chat{}, err
	}
	c.Muted = c.MutedUntil != 0
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

// LIDs lists every LID the store holds as a Chat, a name, an author, a quoted author, a Reaction sender or a System Message target.
func (s *Store) LIDs() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT jid FROM chats WHERE jid LIKE '%@lid' UNION SELECT jid FROM names WHERE jid LIKE '%@lid'
		UNION SELECT sender_jid FROM messages WHERE sender_jid LIKE '%@lid' UNION SELECT quote_sender FROM messages WHERE quote_sender LIKE '%@lid'
		UNION SELECT sender_jid FROM reactions WHERE sender_jid LIKE '%@lid'
		UNION SELECT t.value FROM messages m, json_each(m.system_targets) t WHERE t.value LIKE '%@lid'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lids []string
	for rows.Next() {
		var lid string
		if err := rows.Scan(&lid); err != nil {
			return nil, err
		}
		lids = append(lids, lid)
	}
	return lids, rows.Err()
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

// settingUnarchiveChats is the app_state_settings row of "Keep chats archived" (1: a new Message unarchives its Chat).
const settingUnarchiveChats = "unarchive_chats"

// The bits of chats.live_flags. A bit is set once a live event has set that flag.
const (
	flagPin = 1 << iota
	flagMute
	flagUnread
	flagTimer
	flagArchive
)

// applyFlag stores a Pin, Archive, UnarchiveChats, Mute or ReadMarker event. Other events change nothing.
func applyFlag(db execer, ev whatsapp.Event) error {
	switch ev := ev.(type) {
	case whatsapp.Pin:
		return setFlag(db, "pinned", flagPin, ev.JID, boolInt(ev.Pinned))
	case whatsapp.Archive:
		return setFlag(db, "archived", flagArchive, ev.JID, boolInt(ev.Archived))
	case whatsapp.UnarchiveChats:
		_, err := db.Exec(`INSERT INTO app_state_settings (name, value) VALUES (?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value`, settingUnarchiveChats, boolInt(ev.On))
		return err
	case whatsapp.Mute:
		return setFlag(db, "muted_until", flagMute, ev.JID, ev.Until)
	case whatsapp.ReadMarker:
		if !ev.Unread {
			if err := clearUnreadCount(db, ev.JID); err != nil {
				return err
			}
		}
		return setFlag(db, "unread", flagUnread, ev.JID, boolInt(ev.Unread))
	}
	return nil
}

// readElsewhere stores that another device of the user read these Messages. They leave unreadCount and need no Read Receipt.
// The Read Marker clears when no counted Message is left. It reports whether unreadCount changed.
// A receipt for Messages that no longer count keeps the Read Marker, so a Chat that the phone marked unread stays unread.
func readElsewhere(tx *sql.Tx, ev whatsapp.ReadElsewhere) (bool, error) {
	counted := 0
	for _, id := range ev.IDs {
		res, err := tx.Exec(`UPDATE messages SET in_unread_count = 0 WHERE chat_jid = ? AND id = ? AND in_unread_count = 1`, ev.Chat, id)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		counted += int(n)
		if _, err := tx.Exec(`UPDATE messages SET read_pending = 0 WHERE chat_jid = ? AND id = ?`, ev.Chat, id); err != nil {
			return false, err
		}
	}
	if counted == 0 {
		return false, nil
	}
	_, err := tx.Exec(`UPDATE chats SET unread = 0, live_flags = live_flags | ? WHERE jid = ? AND unread = 1
		AND NOT EXISTS (SELECT 1 FROM messages WHERE chat_jid = ? AND in_unread_count = 1)`, flagUnread, ev.Chat, ev.Chat)
	return true, err
}

// clearUnreadCount takes all Messages of a Chat out of unreadCount. It runs where the Read Marker clears.
func clearUnreadCount(db execer, chat string) error {
	_, err := db.Exec(`UPDATE messages SET in_unread_count = 0 WHERE chat_jid = ? AND in_unread_count = 1`, chat)
	return err
}

// setFlag sets one live column of a Chat and marks it live. column is a fixed string from this file.
// App State can come before the History Sync batch that holds the Chat, so a Chat that is not stored yet gets a row.
// That row is not listed until it has a Message or a Draft. Its kind is direct until a Message sets it.
func setFlag(db execer, column string, bit int, jid string, value int64) error {
	_, err := db.Exec("INSERT INTO chats (jid, "+column+", live_flags) VALUES (?, ?, ?) "+
		"ON CONFLICT(jid) DO UPDATE SET "+column+" = excluded."+column+", live_flags = live_flags | excluded.live_flags",
		jid, value, bit)
	return err
}

// putChat returns the number of Messages of the Chat that were new.
func putChat(tx *sql.Tx, c whatsapp.HistoryChat) (added int, err error) {
	last := c.LastActive
	for _, m := range c.Messages {
		if m.System == nil { // a System Message does not move its Chat
			last = max(last, m.Time)
		}
	}
	_, err = tx.Exec(`
		INSERT INTO chats (jid, kind, subject, last_active, pinned, archived, muted_until, unread, timer) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET kind = excluded.kind,
		  subject = CASE WHEN excluded.subject = '' THEN subject ELSE excluded.subject END,
		  last_active = MAX(last_active, excluded.last_active),
		  pinned = CASE WHEN live_flags & :pin THEN pinned ELSE excluded.pinned END,
		  archived = CASE WHEN live_flags & :archive THEN archived ELSE excluded.archived END,
		  muted_until = CASE WHEN live_flags & :mute THEN muted_until ELSE excluded.muted_until END,
		  unread = CASE WHEN live_flags & :unread THEN unread ELSE excluded.unread END,
		  timer = CASE WHEN live_flags & :timer THEN timer ELSE excluded.timer END`, // a flag that a live event set stays: live App State is newer than a History Sync batch
		c.JID, kindOrDirect(c.Kind), c.Subject, last, boolInt(c.Pinned), boolInt(c.Archived), c.MutedUntil, boolInt(c.Unread), c.Timer,
		sql.Named("pin", flagPin), sql.Named("archive", flagArchive), sql.Named("mute", flagMute), sql.Named("unread", flagUnread), sql.Named("timer", flagTimer))
	if err != nil {
		return 0, err
	}
	for _, m := range c.Messages {
		inserted, err := putMessage(tx, c.JID, m, MediaNone)
		if err != nil {
			return added, err
		}
		if inserted {
			added++
		}
	}
	return added, nil
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
