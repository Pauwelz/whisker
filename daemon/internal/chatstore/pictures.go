package chatstore

import (
	"database/sql"
	"errors"
)

// Picture is the cached Profile picture of a Chat. An empty ID and no Data mean the Chat has no picture.
type Picture struct {
	ID        string
	Data      []byte
	FetchedAt int64 // Unix seconds
}

// Picture returns the cached picture of a Chat. ok is false when nothing is cached.
func (s *Store) Picture(jid string) (p Picture, ok bool, err error) {
	err = s.db.QueryRow(`SELECT picture_id, data, fetched_at FROM pictures WHERE jid = ?`, jid).Scan(&p.ID, &p.Data, &p.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Picture{}, false, nil
	}
	return p, err == nil, err
}

// PutPicture caches the picture of a Chat as fetched now. An empty id and no data cache "no picture".
func (s *Store) PutPicture(jid, id string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO pictures (jid, picture_id, data, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET picture_id = excluded.picture_id, data = excluded.data, fetched_at = excluded.fetched_at`,
		jid, id, data, s.now().Unix())
	return err
}

// TouchPicture marks the cached picture of a Chat as checked now.
func (s *Store) TouchPicture(jid string) error {
	_, err := s.db.Exec(`UPDATE pictures SET fetched_at = ? WHERE jid = ?`, s.now().Unix(), jid)
	return err
}

func (s *Store) DeletePicture(jid string) error {
	_, err := s.db.Exec(`DELETE FROM pictures WHERE jid = ?`, jid)
	return err
}
