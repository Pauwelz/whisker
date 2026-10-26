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

// The size limits of automatic download. "any" has no limit.
const (
	AutoDownloadLimit1MB  = "1mb"
	AutoDownloadLimit5MB  = "5mb"
	AutoDownloadLimit25MB = "25mb"
	AutoDownloadLimitAny  = "any"
)

// autoDownloadBytes is the largest size of each limit in bytes. 0 means no limit.
var autoDownloadBytes = map[string]int64{
	AutoDownloadLimit1MB: 1 << 20, AutoDownloadLimit5MB: 5 << 20, AutoDownloadLimit25MB: 25 << 20, AutoDownloadLimitAny: 0,
}

// ErrInvalid means a setting has a value that is not allowed.
var ErrInvalid = errors.New("invalid setting")

// Settings are the v1 Profile Settings. The JSON tags are the wire names.
type Settings struct {
	ReadReceipts  string `json:"readReceipts"`
	SendOnline    bool   `json:"sendOnline"`
	SendTyping    bool   `json:"sendTyping"`
	Notifications bool   `json:"notifications"`
	// NotificationPreview puts the sender and text in a notification. Off, a notification says "New message".
	NotificationPreview bool `json:"notificationPreview"`
	AutoDownload        bool `json:"autoDownload"` // images, stickers and voice notes download by themselves
	// AutoDownloadLimit is the largest Media that downloads by itself, one of the limits above.
	AutoDownloadLimit string `json:"autoDownloadLimit"`
}

// AutoDownloads tells whether Media of size bytes downloads by itself. An unknown size (0) does.
func (s Settings) AutoDownloads(size int64) bool {
	limit := autoDownloadBytes[s.AutoDownloadLimit]
	return s.AutoDownload && (limit == 0 || size <= limit)
}

// Update is a partial change. A nil field stays as it is.
type Update struct {
	ReadReceipts        *string `json:"readReceipts"`
	SendOnline          *bool   `json:"sendOnline"`
	SendTyping          *bool   `json:"sendTyping"`
	Notifications       *bool   `json:"notifications"`
	NotificationPreview *bool   `json:"notificationPreview"`
	AutoDownload        *bool   `json:"autoDownload"`
	AutoDownloadLimit   *string `json:"autoDownloadLimit"`
}

// Defaults returns the settings of a Profile that has no settings file.
func Defaults() Settings {
	return Settings{ReadReceipts: ReadReceiptsOnOpen, SendOnline: true, SendTyping: true, Notifications: true, NotificationPreview: true, AutoDownload: true, AutoDownloadLimit: AutoDownloadLimit5MB}
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
	if u.NotificationPreview != nil {
		cur.NotificationPreview = *u.NotificationPreview
	}
	if u.AutoDownload != nil {
		cur.AutoDownload = *u.AutoDownload
	}
	if u.AutoDownloadLimit != nil {
		if _, ok := autoDownloadBytes[*u.AutoDownloadLimit]; !ok {
			return cur, ErrInvalid
		}
		cur.AutoDownloadLimit = *u.AutoDownloadLimit
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
	if _, ok := autoDownloadBytes[cur.AutoDownloadLimit]; !ok {
		cur.AutoDownloadLimit = Defaults().AutoDownloadLimit
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
