package chatstore

import "database/sql"

// ClearChat deletes the Messages of a Chat up to until (Unix seconds, 0 for all), with their Media and Reactions.
// The Chat, its Draft and its settings stay. It reports whether a Message went, so a repeat changes nothing.
func (s *Store) ClearChat(chat string, until int64) (bool, error) {
	rows, err := s.db.Query(`SELECT id FROM messages WHERE chat_jid = ? AND (? = 0 OR time <= ?)`, chat, until, until)
	if err != nil {
		return false, err
	}
	ids, err := scanStrings(rows)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if err := s.DeleteMessage(chat, id); err != nil {
			return false, err
		}
	}
	return len(ids) > 0, nil
}

// HasMessageAfter reports whether the Chat holds a Message with a time after until (Unix seconds).
func (s *Store) HasMessageAfter(chat string, until int64) (has bool, err error) {
	err = s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM messages WHERE chat_jid = ? AND time > ?)`, chat, until).Scan(&has)
	return has, err
}

// DeleteChat deletes a Chat and every row that its Chat or its Messages key, with the Media files.
// The names stay: they belong to people. It returns the Attachments of the Draft, for the caller to drop,
// and reports whether anything went, so a repeat changes nothing.
func (s *Store) DeleteChat(chat string) (attachments []string, gone bool, err error) {
	draft, err := s.Draft(chat)
	if err != nil {
		return nil, false, err
	}
	gone, err = s.ClearChat(chat, 0)
	if err != nil {
		return nil, false, err
	}
	rows, err := s.db.Query(`SELECT id FROM media WHERE chat_jid = ?`, chat) // a Media row that outlived its Message
	if err != nil {
		return nil, false, err
	}
	orphans, err := scanStrings(rows)
	if err != nil {
		return nil, false, err
	}
	for _, id := range orphans {
		if _, err := s.DeleteMedia(chat, id); err != nil {
			return nil, false, err
		}
	}
	err = s.inTx(func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM reactions WHERE chat_jid = ?`,
			`DELETE FROM recipients WHERE chat_jid = ?`,
			`DELETE FROM group_receipts WHERE chat_jid = ?`,
			`DELETE FROM drafts WHERE chat_jid = ?`,
			`DELETE FROM pictures WHERE jid = ?`,
			`DELETE FROM chats WHERE jid = ?`,
		} {
			res, err := tx.Exec(q, chat)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				gone = true
			}
		}
		return nil
	})
	return draft.Attachments, gone, err
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
