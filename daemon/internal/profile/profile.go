// Package profile owns the Profile on disk: the encrypted device store and chat store, and the store key.
package profile

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
// A store that the key cannot open is unreadable. This happens when the key is new but the Profile directory exists (lost key),
// and when the key is in the keyring but is wrong. Open then deletes the old stores and starts a fresh Profile.
// A keyring error leaves the Profile alone and returns keystore.ErrUnavailable.
// Then Open gives the Media store its name key and renames the Media files of the old layout.
func (p *Profile) Open() error {
	key, err := p.openKeyed()
	if err != nil {
		return err
	}
	p.Media().SetNameKey(media.NameKey(key))
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
		if err := p.removeData(); err != nil {
			return nil, err
		}
	}
	err = p.openStores(key)
	if err == nil || created || !errors.Is(err, sqlite3.NOTADB) {
		return key, err
	}
	// Wrong key: replace it, because the old one opens nothing.
	if err := p.Delete(); err != nil {
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
// Profile Settings live in $XDG_CONFIG_HOME and stay.
func (p *Profile) Delete() error {
	if err := p.removeData(); err != nil {
		return err
	}
	return p.keys.Delete()
}

// removeData closes the stores and removes the Profile directory. Delete and Open (lost key) share it.
func (p *Profile) removeData() error {
	p.Close()
	return os.RemoveAll(p.Dir)
}

// dsn opens an adiantum-encrypted file. whatsmeow needs foreign keys on. Setting any _pragma drops the default busy timeout.
// secure_delete(1) overwrites deleted rows in the file. temp_store(2) keeps the temporary copy of VACUUM in memory, not in a plain file.
func dsn(path, hexKey string) string {
	return "file:" + path + "?vfs=adiantum&hexkey=" + hexKey + "&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=temp_store(2)&_pragma=secure_delete(1)"
}
