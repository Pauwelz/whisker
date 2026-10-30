package chatstore

import (
	"strings"
	"time"
)

// retryMaxAge is whatsmeow's age limit for a sent Message in its retry store.
const retryMaxAge = 7 * 24 * time.Hour

// AddRetry keeps a sent Message proto for a retry receipt. targetID is the id that an edit, a revoke or a reaction acts on, or "".
func (s *Store) AddRetry(chat, id, targetID, format string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO retry_messages (chat_jid, id, target_id, format, data, timestamp) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)
		ON CONFLICT (chat_jid, id) DO UPDATE SET target_id = excluded.target_id, format = excluded.format, data = excluded.data, timestamp = excluded.timestamp`,
		chat, id, targetID, format, data, s.now().UnixMilli())
	return err
}

// Retry returns the stored proto of a sent Message in the Chat or in its alternate JID (phone number or LID).
// A Message that is not there gives sql.ErrNoRows, as whatsmeow's own store does.
func (s *Store) Retry(chat, altChat, id string) (format string, data []byte, err error) {
	err = s.db.QueryRow(`SELECT format, data FROM retry_messages WHERE (chat_jid = ? OR chat_jid = ?) AND id = ?`, chat, altChat, id).Scan(&format, &data)
	return format, data, err
}

// deleteOldRetries deletes the stored Messages that are older than the age limit. The expiry sweep calls it.
func (s *Store) deleteOldRetries() error {
	_, err := s.db.Exec(`DELETE FROM retry_messages WHERE timestamp < ?`, s.now().Add(-retryMaxAge).UnixMilli())
	return err
}

// deleteRetries deletes the stored Messages with these ids, and the ones that act on them (edit, revoke, reaction).
// It keys by id, not by Chat: the JID that whatsmeow sent to can differ from the Chat of the Message.
func (s *Store) deleteRetries(ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, 2*len(ids))
	for range 2 {
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.db.Exec(`DELETE FROM retry_messages WHERE id IN (`+marks+`) OR target_id IN (`+marks+`)`, args...)
	return err
}
