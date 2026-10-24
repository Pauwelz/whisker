package chatstore

import "database/sql"

// MergeChat folds the LID Chat into the phone Chat once WhatsApp has said that both are one Participant.
// It moves the Messages, Reactions, Media rows and the Draft, merges the flags and moves the names.
// A row that the phone Chat already has wins: the same Message id, the Draft. With no phone Chat, the LID Chat gets the phone JID.
// It reports false when nothing named the LID, so a repeated alias costs two queries.
func (s *Store) MergeChat(lid, phone string) (changed bool, err error) {
	if lid == phone {
		return false, nil
	}
	err = s.inTx(func(tx *sql.Tx) error {
		var known int
		if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM chats WHERE jid = :lid) + (SELECT COUNT(*) FROM names WHERE jid = :lid)`, sql.Named("lid", lid)).Scan(&known); err != nil {
			return err
		}
		if known == 0 {
			return nil
		}
		changed = true
		steps := []string{
			// Copy the LID Chat under the phone JID when the phone Chat is absent, then merge the flags. -1 (muted for ever) beats any end time.
			`INSERT OR IGNORE INTO chats (jid, kind, subject, last_active, pinned, muted_until, unread, timer)
				SELECT :phone, kind, subject, last_active, pinned, muted_until, unread, timer FROM chats WHERE jid = :lid`,
			`UPDATE chats SET
				last_active = MAX(chats.last_active, l.last_active), pinned = chats.pinned OR l.pinned, unread = chats.unread OR l.unread,
				muted_until = CASE WHEN chats.muted_until = -1 OR l.muted_until = -1 THEN -1 ELSE MAX(chats.muted_until, l.muted_until) END,
				timer = CASE WHEN chats.timer <> 0 THEN chats.timer ELSE l.timer END
				FROM (SELECT * FROM chats WHERE jid = :lid) AS l WHERE chats.jid = :phone`,
			`DELETE FROM chats WHERE jid = :lid`,
			// UPDATE OR IGNORE skips a row whose key the phone Chat has; the DELETE then drops that leftover.
			`UPDATE OR IGNORE messages SET chat_jid = :phone WHERE chat_jid = :lid`,
			`DELETE FROM messages WHERE chat_jid = :lid`,
			`UPDATE OR IGNORE reactions SET chat_jid = :phone WHERE chat_jid = :lid`,
			`DELETE FROM reactions WHERE chat_jid = :lid`,
			`UPDATE OR IGNORE media SET chat_jid = :phone WHERE chat_jid = :lid`,
			`DELETE FROM media WHERE chat_jid = :lid`,
			`UPDATE OR IGNORE drafts SET chat_jid = :phone WHERE chat_jid = :lid`,
			`DELETE FROM drafts WHERE chat_jid = :lid`,
			// Authors and quoted authors in any Chat.
			`UPDATE messages SET sender_jid = :phone WHERE sender_jid = :lid`,
			`UPDATE messages SET quote_sender = :phone WHERE quote_sender = :lid`,
			`UPDATE OR IGNORE reactions SET sender_jid = :phone WHERE sender_jid = :lid`,
			`DELETE FROM reactions WHERE sender_jid = :lid`,
			// A name the phone row holds wins.
			`INSERT INTO names (jid, contact_name, push_name) SELECT :phone, contact_name, push_name FROM names WHERE jid = :lid
				ON CONFLICT (jid) DO UPDATE SET
				  contact_name = CASE WHEN names.contact_name = '' THEN excluded.contact_name ELSE names.contact_name END,
				  push_name = CASE WHEN names.push_name = '' THEN excluded.push_name ELSE names.push_name END`,
			`DELETE FROM names WHERE jid = :lid`,
		}
		for _, q := range steps {
			if _, err := tx.Exec(q, sql.Named("lid", lid), sql.Named("phone", phone)); err != nil {
				return err
			}
		}
		return nil
	})
	return changed && err == nil, err
}
