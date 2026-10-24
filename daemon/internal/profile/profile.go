// Package profile owns the Profile on disk: the encrypted device store and chat store, and the store key.
package profile

import (
	"context"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"       // registers the "sqlite3" database/sql driver
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum" // registers the "adiantum" VFS
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/Pauwelz/whisker/daemon/internal/keystore"
)

type Profile struct {
	Dir  string // $XDG_DATA_HOME/whisker
	keys *keystore.Store

	device *sqlstore.Container
	chat   *sql.DB
}

func New(dir string, keys *keystore.Store) *Profile { return &Profile{Dir: dir, keys: keys} }

// Chat returns the chat store. It is valid after a successful Open.
func (p *Profile) Chat() *sql.DB { return p.chat }

// Device returns the whatsmeow device store. It is valid after a successful Open.
func (p *Profile) Device() *sqlstore.Container { return p.device }

// Open loads the store key and opens both stores with it.
// If the key is new but the Profile directory exists, the key was lost: the old stores are unreadable, so Open deletes them.
// A keyring error leaves the Profile alone and returns keystore.ErrUnavailable.
func (p *Profile) Open() error {
	key, created, err := p.keys.Load()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(p.Dir); created && statErr == nil {
		if err := os.RemoveAll(p.Dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return err
	}
	hexKey := hex.EncodeToString(key)

	p.device, err = sqlstore.New(context.Background(), "sqlite3", dsn(filepath.Join(p.Dir, "device.db"), hexKey), waLog.Noop)
	if err != nil {
		return err
	}
	p.chat, err = sql.Open("sqlite3", dsn(filepath.Join(p.Dir, "chat.db"), hexKey))
	if err != nil {
		p.Close()
		return err
	}
	// Reading the header proves the key is right.
	if err := p.chat.QueryRow("PRAGMA user_version").Scan(new(int)); err != nil {
		p.Close()
		return err
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
	p.Close()
	if err := os.RemoveAll(p.Dir); err != nil {
		return err
	}
	return p.keys.Delete()
}

// dsn opens an adiantum-encrypted file. whatsmeow needs foreign keys on. Setting any _pragma drops the default busy timeout.
func dsn(path, hexKey string) string {
	return "file:" + path + "?vfs=adiantum&hexkey=" + hexKey + "&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)"
}
