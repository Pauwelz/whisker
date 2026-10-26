// Package profile owns the Profile on disk: the encrypted device store and chat store, and the store key.
package profile

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"       // registers the "sqlite3" database/sql driver
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum" // registers the "adiantum" VFS
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
)

type Profile struct {
	Dir   string // $XDG_DATA_HOME/whisker
	keys  *keystore.Store
	media *media.Store

	device *sqlstore.Container
	chat   *sql.DB

	vacuumed   bool         // the last Open ran the one-time VACUUM; the tests read it
	vacuumHook func() error // the tests make VACUUM fail with it
}

func New(dir string, keys *keystore.Store) *Profile { return &Profile{Dir: dir, keys: keys} }

// Chat returns the chat store. It is valid after a successful Open.
func (p *Profile) Chat() *sql.DB { return p.chat }

// Media returns the Media of the Profile: encrypted files in the Profile, plain copies in $XDG_RUNTIME_DIR/whisker/media.
// Without XDG_RUNTIME_DIR the store refuses plain copies (the Daemon exits at start without it).
// Open gives the store its name key. Before Open the store can only Wipe.
func (p *Profile) Media() *media.Store {
	if p.media == nil {
		plain := ""
		if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
			plain = filepath.Join(rt, "whisker", "media")
		}
		p.media = media.New(filepath.Join(p.Dir, "media"), plain)
	}
	return p.media
}

// Device returns the whatsmeow device store. It is valid after a successful Open.
func (p *Profile) Device() *sqlstore.Container { return p.device }

// Open loads the store key and opens both stores with it.
// A store that the key cannot open is unreadable. This happens when the key is new but store files exist (lost key),
// and when the key is in the keyring but is wrong. Open never deletes such a Profile: it renames the Profile directory
// to <dir>.unreadable-<time> and starts a fresh Profile (see MovedAside).
// A keyring error leaves the Profile alone and returns keystore.ErrUnavailable.
// Then Open gives the Media store its name key and file key, and renames the Media files of the old layout.
func (p *Profile) Open() error {
	key, err := p.openKeyed()
	if err != nil {
		return err
	}
	p.Media().SetNameKey(media.NameKey(key))
	p.Media().SetFileKey(media.FileKey(key))
	_ = p.Media().Rename() // a file that did not move stays, and the next Open tries again
	return nil
}

// openKeyed opens the stores and returns the key that opened them.
func (p *Profile) openKeyed() ([]byte, error) {
	key, created, err := p.keys.Load()
	if err != nil {
		return nil, err
	}
	if created {
		if err := p.setAside(); err != nil {
			return nil, err
		}
	}
	err = p.openStores(key)
	if err == nil || created || !errors.Is(err, sqlite3.NOTADB) {
		return key, err
	}
	// Wrong key: replace it, because the old one opens nothing.
	if err := p.setAside(); err != nil {
		return nil, err
	}
	if err := p.keys.Delete(); err != nil {
		return nil, err
	}
	key, _, err = p.keys.Load()
	if err != nil {
		return nil, err
	}
	return key, p.openStores(key)
}

func (p *Profile) openStores(key []byte) (err error) {
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return err
	}
	hexKey := hex.EncodeToString(key)

	p.device, err = sqlstore.New(context.Background(), "sqlite3", dsn(filepath.Join(p.Dir, "device.db"), hexKey), waLog.Noop)
	if err != nil {
		p.Close()
		return err
	}
	p.chat, err = sql.Open("sqlite3", dsn(filepath.Join(p.Dir, "chat.db"), hexKey))
	if err != nil {
		p.Close()
		return err
	}
	// Reading the version also proves the key is right. Version 0 is a new store: it has no residue.
	var before int
	if err := p.chat.QueryRow("PRAGMA user_version").Scan(&before); err != nil {
		p.Close()
		return err
	}
	// VACUUM runs before Migrate: the version reaches 25 only when both stores are clean, so a failed VACUUM runs again at the next Open.
	p.vacuumed = before > 0 && before < vacuumVersion
	if p.vacuumed {
		if err := p.vacuum(hexKey); err != nil {
			p.Close()
			return err
		}
	}
	if err := chatstore.Migrate(p.chat); err != nil {
		p.Close()
		return err
	}
	return nil
}

// vacuumVersion is the chat store version of migration 0025_vacuum.sql.
// A store below it was written without secure_delete, so its free space can hold deleted data.
const vacuumVersion = 25

// vacuum rewrites both stores once, which drops the old free space. VACUUM cannot run in a migration transaction.
func (p *Profile) vacuum(hexKey string) error {
	if p.vacuumHook != nil {
		if err := p.vacuumHook(); err != nil {
			return err
		}
	}
	for _, name := range []string{"chat.db", "device.db"} {
		db, err := sql.Open("sqlite3", dsn(filepath.Join(p.Dir, name), hexKey))
		if err != nil {
			return err
		}
		_, err = db.Exec("VACUUM")
		db.Close()
		if err != nil {
			return fmt.Errorf("vacuum %s: %w", name, err)
		}
	}
	return nil
}

// Close closes both stores.
func (p *Profile) Close() {
	if p.device != nil {
		_ = p.device.Close()
		p.device = nil
	}
	if p.chat != nil {
		_ = p.chat.Close()
		p.chat = nil
	}
}

// Delete is the one routine that deletes a Profile: the stores, the Media and the store key.
// It keeps the unreadable Profiles: a phone unlink must not remove them (see RemoveUnreadable).
// Profile Settings live in $XDG_CONFIG_HOME and stay.
func (p *Profile) Delete() error {
	p.Close()
	if err := os.RemoveAll(p.Dir); err != nil {
		return err
	}
	return p.keys.Delete()
}

// RemoveUnreadable removes the Profiles that Open set aside. Log out calls it.
func (p *Profile) RemoveUnreadable() error {
	old, err := p.unreadable()
	if err != nil {
		return err
	}
	for _, dir := range old {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

const unreadableSuffix = ".unreadable-"

// unreadablePrefix starts the name of a Profile directory that Open set aside.
func (p *Profile) unreadablePrefix() string { return filepath.Base(p.Dir) + unreadableSuffix }

// unreadable lists the Profile directories that Open set aside, next to the Profile directory.
func (p *Profile) unreadable() ([]string, error) {
	entries, err := os.ReadDir(filepath.Dir(p.Dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), p.unreadablePrefix()) {
			dirs = append(dirs, filepath.Join(filepath.Dir(p.Dir), e.Name()))
		}
	}
	return dirs, nil
}

// MovedAside tells whether an unreadable Profile waits next to the Profile directory. Log out removes it.
func (p *Profile) MovedAside() bool {
	dirs, _ := p.unreadable()
	return len(dirs) > 0
}

// hasStores tells whether a store file may exist. Only "does not exist" counts as absent, so a stat error never allows a delete.
func (p *Profile) hasStores() bool {
	for _, name := range []string{"device.db", "chat.db"} {
		if _, err := os.Stat(filepath.Join(p.Dir, name)); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

// setAside closes the stores and renames a Profile that holds store files to <dir>.unreadable-<time>.
// Without store files there is no chat to keep, so it removes the directory (it can only hold Media that nothing can read).
func (p *Profile) setAside() error {
	p.Close()
	if !p.hasStores() {
		return os.RemoveAll(p.Dir)
	}
	return os.Rename(p.Dir, p.Dir+unreadableSuffix+time.Now().UTC().Format("20060102T150405.000000"))
}

// dsn opens an adiantum-encrypted file. whatsmeow needs foreign keys on. Setting any _pragma drops the default busy timeout.
// secure_delete(1) overwrites deleted rows in the file. temp_store(2) keeps the temporary copy of VACUUM in memory, not in a plain file.
// net/url escapes the path, so a '#', '?' or '%' in it cannot end the path early and drop vfs=adiantum.
func dsn(path, hexKey string) string {
	q := url.Values{"vfs": {"adiantum"}, "hexkey": {hexKey}, "_pragma": {"foreign_keys(1)", "busy_timeout(10000)", "temp_store(2)", "secure_delete(1)"}}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
}
