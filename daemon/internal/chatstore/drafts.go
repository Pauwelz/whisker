package chatstore

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Draft is the unsent composition of a Chat. Quote and Edit are Message ids, or empty.
// Attachments are the absolute paths of the Attachments, in order. The list is never nil when read.
type Draft struct {
	Text        string   `json:"text"`
	Quote       string   `json:"quote"`
	Edit        string   `json:"edit"`
	Attachments []string `json:"attachments"`
}

// Draft returns the Draft of a Chat. A Chat with no Draft gives the zero Draft.
func (s *Store) Draft(chat string) (Draft, error) {
	d := Draft{Attachments: []string{}}
	var list string
	err := s.db.QueryRow(`SELECT text, quote_id, edit_id, attachments FROM drafts WHERE chat_jid = ?`, chat).Scan(&d.Text, &d.Quote, &d.Edit, &list)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal([]byte(list), &d.Attachments)
}

// SetDraft stores the Draft of a Chat. An empty Draft deletes it.
func (s *Store) SetDraft(chat string, d Draft) error {
	if d.Text == "" && d.Quote == "" && d.Edit == "" && len(d.Attachments) == 0 {
		return s.ClearDraft(chat)
	}
	list, err := json.Marshal(append([]string{}, d.Attachments...)) // nil becomes [], not null
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO drafts (chat_jid, text, quote_id, edit_id, attachments) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (chat_jid) DO UPDATE SET text = excluded.text, quote_id = excluded.quote_id, edit_id = excluded.edit_id, attachments = excluded.attachments`,
		chat, d.Text, d.Quote, d.Edit, string(list))
	return err
}

func (s *Store) ClearDraft(chat string) error {
	_, err := s.db.Exec(`DELETE FROM drafts WHERE chat_jid = ?`, chat)
	return err
}
