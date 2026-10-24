// Package settings owns the Profile Settings: one JSON file in the config folder.
// The Plugin reads and writes them only over JSON-RPC (see docs/contract.md).
// Later tickets read the fields through Store.Get.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Read Receipt modes.
const (
	ReadReceiptsOff           = "off"
	ReadReceiptsOnOpen        = "onOpen"
	ReadReceiptsOnInteraction = "onInteraction"
)

// ErrInvalid means a setting has a value that is not allowed.
var ErrInvalid = errors.New("invalid setting")

// Settings are the v1 Profile Settings. The JSON tags are the wire names.
type Settings struct {
	ReadReceipts  string `json:"readReceipts"`
	SendOnline    bool   `json:"sendOnline"`
	SendTyping    bool   `json:"sendTyping"`
	Notifications bool   `json:"notifications"`
}

// Update is a partial change. A nil field stays as it is.
type Update struct {
	ReadReceipts  *string `json:"readReceipts"`
	SendOnline    *bool   `json:"sendOnline"`
	SendTyping    *bool   `json:"sendTyping"`
	Notifications *bool   `json:"notifications"`
}

// Defaults returns the settings of a Profile that has no settings file.
func Defaults() Settings {
	return Settings{ReadReceipts: ReadReceiptsOnOpen, SendOnline: true, SendTyping: true, Notifications: true}
}

func validMode(m string) bool {
	return m == ReadReceiptsOff || m == ReadReceiptsOnOpen || m == ReadReceiptsOnInteraction
}

// Store keeps the settings in a JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a store for the file at path. The file need not exist.
func New(path string) *Store { return &Store{path: path} }

// Get returns the saved settings. Missing keys take their default; a missing file gives the defaults.
func (s *Store) Get() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Set applies the update, saves the result and returns it. An invalid update changes nothing.
func (s *Store) Set(u Update) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.load()
	if err != nil {
		return cur, err
	}
	if u.ReadReceipts != nil {
		if !validMode(*u.ReadReceipts) {
			return cur, ErrInvalid
		}
		cur.ReadReceipts = *u.ReadReceipts
	}
	if u.SendOnline != nil {
		cur.SendOnline = *u.SendOnline
	}
	if u.SendTyping != nil {
		cur.SendTyping = *u.SendTyping
	}
	if u.Notifications != nil {
		cur.Notifications = *u.Notifications
	}
	return cur, s.save(cur)
}

func (s *Store) load() (Settings, error) {
	cur := Defaults()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return cur, nil
	}
	if err != nil {
		return cur, err
	}
	if err := json.Unmarshal(raw, &cur); err != nil {
		return Defaults(), err
	}
	// A hand-edited file with a bad mode falls back to the default.
	if !validMode(cur.ReadReceipts) {
		cur.ReadReceipts = Defaults().ReadReceipts
	}
	return cur, nil
}

// save writes through a temp file and a rename, so a crash never leaves half a file.
func (s *Store) save(cur Settings) error {
	raw, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
