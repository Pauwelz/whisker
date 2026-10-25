package chatstore

import (
	"database/sql"
	"slices"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// SetRecipients freezes the Recipients of a tracked Message. The set is final, also when it is empty.
func (s *Store) SetRecipients(chat, id string, rs []whatsapp.Participant) error {
	return s.inTx(func(tx *sql.Tx) error {
		var open bool // tracked, and no set yet
		if err := tx.QueryRow(`SELECT track_recipients = 1 AND recipients_set = 0
			FROM messages WHERE chat_jid = ?1 AND id = ?2`, chat, id).Scan(&open); err != nil && err != sql.ErrNoRows {
			return err
		}
		if !open {
			return nil
		}
		if _, err := tx.Exec(`UPDATE messages SET recipients_set = 1 WHERE chat_jid = ? AND id = ?`, chat, id); err != nil {
			return err
		}
		for _, r := range rs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO recipients (chat_jid, id, phone, lid) VALUES (?, ?, ?, ?)`, chat, id, r.Phone, r.LID); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordReceipt keeps a receipt of a Group Chat. It reports whether the Message is tracked: History Sync Messages are not.
// A History Sync Message keeps the state that History Sync carries and gets no set. WhatsApp does not send again
// the receipts that arrived before whisker was linked, so a set for it would never complete.
// sender is a phone JID or a LID without a device part.
func (s *Store) RecordReceipt(chat, id, sender, state string) (bool, error) {
	var tracked bool
	err := s.db.QueryRow(`SELECT track_recipients = 1 FROM messages WHERE chat_jid = ? AND id = ?`, chat, id).Scan(&tracked)
	if err == sql.ErrNoRows || !tracked {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO group_receipts (chat_jid, id, sender, state) VALUES (?, ?, ?, ?)`, chat, id, sender, state)
	return err == nil, err
}

// Combined is the lowest state that every Recipient reached: delivered, read or played, or "" when one has reached none.
// known is false until the set is frozen. A frozen empty set is known, and the Message never moves.
func (s *Store) Combined(chat, id string) (state string, known bool, err error) {
	err = s.db.QueryRow(`SELECT recipients_set = 1 FROM messages WHERE chat_jid = ? AND id = ?`, chat, id).Scan(&known)
	if err == sql.ErrNoRows || !known {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var rs []whatsapp.Participant
	if err := s.scan(`SELECT phone, lid FROM recipients WHERE chat_jid = ? AND id = ?`, []any{chat, id}, func(r *sql.Rows) error {
		var x whatsapp.Participant
		err := r.Scan(&x.Phone, &x.LID)
		rs = append(rs, x)
		return err
	}); err != nil || len(rs) == 0 {
		return "", known, err
	}
	reached := map[string]int{} // sender -> highest index in stateOrder
	if err := s.scan(`SELECT sender, state FROM group_receipts WHERE chat_jid = ? AND id = ?`, []any{chat, id}, func(r *sql.Rows) error {
		var sender, st string
		if err := r.Scan(&sender, &st); err != nil {
			return err
		}
		reached[sender] = max(reached[sender], slices.Index(stateOrder, st))
		return nil
	}); err != nil {
		return "", false, err
	}
	lowest := len(stateOrder)
	for _, r := range rs {
		lowest = min(lowest, max(reached[r.Phone], reached[r.LID]))
	}
	if lowest < slices.Index(stateOrder, whatsapp.StateDelivered) {
		return "", true, nil
	}
	return stateOrder[lowest], true, nil
}

// scan runs a query and calls each for every row.
func (s *Store) scan(query string, args []any, each func(*sql.Rows) error) error {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
