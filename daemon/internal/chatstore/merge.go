package chatstore

import (
	"database/sql"
	"strconv"
)

// MergeChat folds the LID Chat into the phone Chat once WhatsApp has said that both are one Participant.
// It moves the Messages, Reactions, Media rows and the Draft, merges the flags and moves the names.
// A row that the phone Chat already has wins: the same Message id. Two Drafts keep the phone targets and join the texts (phone, newline, LID). With no phone Chat, the LID Chat gets the phone JID.
// It reports false when nothing named the LID, so a repeated alias costs two queries.
func (s *Store) MergeChat(lid, phone string) (changed bool, err error) {
	if lid == phone {
		return false, nil
	}
	err = s.inTx(func(tx *sql.Tx) error {
		var known int
		// Count every place the LID can be, so an orphan Draft or a LID that is only a group sender still merges.
		if err := tx.QueryRow(`SELECT
			(SELECT COUNT(*) FROM chats WHERE jid = :lid) + (SELECT COUNT(*) FROM names WHERE jid = :lid) +
			(SELECT COUNT(*) FROM drafts WHERE chat_jid = :lid) +
			(SELECT COUNT(*) FROM messages WHERE chat_jid = :lid OR sender_jid = :lid OR quote_sender = :lid OR instr(system_targets, '"' || :lid || '"')) +
			(SELECT COUNT(*) FROM reactions WHERE chat_jid = :lid OR sender_jid = :lid)`, sql.Named("lid", lid)).Scan(&known); err != nil {
			return err
		}
		if known == 0 {
			return nil
		}
		changed = true
		pin := strconv.Itoa(flagPin) // a live pin is newer than a History Sync pin, so it wins the merge
		archive := strconv.Itoa(flagArchive)
		steps := []string{
			// Two Drafts keep the phone targets (Quote, Edit, file) and join the texts, so no typed text is lost. A lone Draft just moves below.
			`UPDATE drafts SET text = CASE WHEN drafts.text = '' THEN l.text WHEN l.text = '' THEN drafts.text ELSE drafts.text || char(10) || l.text END
				FROM (SELECT text FROM drafts WHERE chat_jid = :lid) AS l WHERE drafts.chat_jid = :phone`,
			// Copy the LID Chat under the phone JID when the phone Chat is absent, then merge the flags. -1 (muted for ever) beats any end time.
			`INSERT OR IGNORE INTO chats (jid, kind, subject, last_active, pinned, archived, muted_until, unread, timer, live_flags)
				SELECT :phone, kind, subject, last_active, pinned, archived, muted_until, unread, timer, live_flags FROM chats WHERE jid = :lid`,
			`UPDATE chats SET
				live_flags = chats.live_flags | l.live_flags,
				last_active = MAX(chats.last_active, l.last_active), unread = chats.unread OR l.unread,
				pinned = CASE WHEN chats.live_flags & ` + pin + ` THEN chats.pinned WHEN l.live_flags & ` + pin + ` THEN l.pinned ELSE chats.pinned OR l.pinned END,
				archived = CASE WHEN chats.live_flags & ` + archive + ` THEN chats.archived WHEN l.live_flags & ` + archive + ` THEN l.archived ELSE chats.archived AND l.archived END,
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
			// The targets are a JSON array of strings, so the quoted LID matches one whole target.
			`UPDATE messages SET system_targets = replace(system_targets, '"' || :lid || '"', '"' || :phone || '"') WHERE instr(system_targets, '"' || :lid || '"')`,
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
