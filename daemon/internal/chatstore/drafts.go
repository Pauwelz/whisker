package chatstore

import (
	"database/sql"
	"errors"
)

// Draft is the unsent composition of a Chat. Quote and Edit are Message ids, or empty.
type Draft struct {
	Text  string `json:"text"`
	Quote string `json:"quote"`
	Edit  string `json:"edit"`
}

// Draft returns the Draft of a Chat. A Chat with no Draft gives the zero Draft.
func (s *Store) Draft(chat string) (Draft, error) {
	var d Draft
	err := s.db.QueryRow(`SELECT text, quote_id, edit_id FROM drafts WHERE chat_jid = ?`, chat).Scan(&d.Text, &d.Quote, &d.Edit)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, nil
	}
	return d, err
}

// SetDraft stores the Draft of a Chat. An empty Draft deletes it.
func (s *Store) SetDraft(chat string, d Draft) error {
	if d == (Draft{}) {
		return s.ClearDraft(chat)
	}
	_, err := s.db.Exec(`INSERT INTO drafts (chat_jid, text, quote_id, edit_id) VALUES (?, ?, ?, ?)
		ON CONFLICT (chat_jid) DO UPDATE SET text = excluded.text, quote_id = excluded.quote_id, edit_id = excluded.edit_id`,
		chat, d.Text, d.Quote, d.Edit)
	return err
}

func (s *Store) ClearDraft(chat string) error {
	_, err := s.db.Exec(`DELETE FROM drafts WHERE chat_jid = ?`, chat)
	return err
}
