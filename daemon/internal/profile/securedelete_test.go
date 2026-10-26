package profile

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lukechampine.com/adiantum"

	"github.com/Pauwelz/whisker/daemon/internal/chatstore"
	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const chatJID = "a@s.whatsapp.net"

func openProfile(t *testing.T) (*Profile, []byte) {
	t.Helper()
	keys := keystore.New(keystore.NewFake(), clock.Real)
	p := New(filepath.Join(t.TempDir(), "whisker"), keys)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	key, _, err := keys.Load()
	if err != nil {
		t.Fatal(err)
	}
	return p, key
}

// decrypted returns the plain bytes of an adiantum file: 4096-byte blocks, tweak = little-endian block offset.
func decrypted(t *testing.T, path string, key []byte) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c := adiantum.New(key)
	var tweak [8]byte
	var plain []byte
	for off := 0; off+4096 <= len(raw); off += 4096 {
		binary.LittleEndian.PutUint64(tweak[:], uint64(off))
		plain = append(plain, c.Decrypt(append([]byte(nil), raw[off:off+4096]...), tweak[:])...)
	}
	return plain
}

func putMessage(t *testing.T, db *sql.DB, id, text string, timer int64) *chatstore.Store {
	t.Helper()
	s := chatstore.New(db, time.Now)
	ev := whatsapp.NewMessage{Chat: chatJID, Message: whatsapp.Message{ID: id, Sender: chatJID, Time: 1000, Text: text, Timer: timer}}
	if _, err := s.PutLive(ev, chatstore.LivePolicy{MediaState: chatstore.MediaNone}); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertGone(t *testing.T, p *Profile, key []byte, marker string) {
	t.Helper()
	p.Close()
	path := filepath.Join(p.Dir, "chat.db")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(marker)) || bytes.Contains(decrypted(t, path, key), []byte(marker)) {
		t.Errorf("chat.db still holds %q", marker)
	}
}

func TestDeleteMessageLeavesNoResidue(t *testing.T) {
	p, key := openProfile(t)
	const marker = "residue-marker-delete-91c4"
	s := putMessage(t, p.Chat(), "m1", marker, 0)
	if err := s.DeleteMessage(chatJID, "m1"); err != nil {
		t.Fatal(err)
	}
	assertGone(t, p, key, marker)
}

func TestExpireLeavesNoResidue(t *testing.T) {
	p, key := openProfile(t)
	const marker = "residue-marker-expire-5be2"
	s := putMessage(t, p.Chat(), "m1", marker, 60)
	gone, err := s.Expire(1000 + 61)
	if err != nil || len(gone) != 1 {
		t.Fatalf("Expire = %v, %v; want one Message", gone, err)
	}
	assertGone(t, p, key, marker)
}

func TestDsnSetsSecureDelete(t *testing.T) {
	p, key := openProfile(t)
	defer p.Close()
	hexKey := hex.EncodeToString(key)
	for _, name := range []string{"chat.db", "device.db"} {
		db, err := sql.Open("sqlite3", dsn(filepath.Join(p.Dir, name), hexKey))
		if err != nil {
			t.Fatal(err)
		}
		var v int
		if err := db.QueryRowContext(context.Background(), "PRAGMA secure_delete").Scan(&v); err != nil || v != 1 {
			t.Errorf("%s: secure_delete = %d, %v; want 1", name, v, err)
		}
		db.Close()
	}
}

// oldStore makes a chat store at version 24 that holds the residue of a deleted Message, as an old Daemon left it.
func oldStore(t *testing.T, marker string) (*Profile, []byte) {
	t.Helper()
	p, key := openProfile(t)
	p.Close()
	old, err := sql.Open("sqlite3", "file:"+filepath.Join(p.Dir, "chat.db")+"?vfs=adiantum&hexkey="+hex.EncodeToString(key)+"&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	s := putMessage(t, old, "m1", marker, 0)
	if err := s.DeleteMessage(chatJID, "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("PRAGMA user_version = 24"); err != nil {
		t.Fatal(err)
	}
	old.Close()
	if !bytes.Contains(decrypted(t, filepath.Join(p.Dir, "chat.db"), key), []byte(marker)) {
		t.Fatal("test setup: the old store holds no residue")
	}
	return p, key
}

func TestOpenVacuumsAnOldStoreOnce(t *testing.T) {
	const marker = "residue-marker-vacuum-27d8"
	p, key := oldStore(t, marker)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if !p.vacuumed {
		t.Error("first Open did not run VACUUM")
	}
	p.Close()
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if p.vacuumed {
		t.Error("second Open ran VACUUM again")
	}
	assertGone(t, p, key, marker)
}

func TestNewStoreRunsNoVacuum(t *testing.T) {
	p, _ := openProfile(t)
	defer p.Close()
	if p.vacuumed {
		t.Error("a new store ran VACUUM")
	}
}

// Guards against a future VFS that writes plain temp files: today the adiantum VFS encrypts them too.
func TestVacuumWritesNoPlainTempFile(t *testing.T) {
	const marker = "residue-marker-temp-6a01"
	p, _ := oldStore(t, marker)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("SQLITE_TMPDIR", tmp)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if !p.vacuumed {
		t.Fatal("VACUUM did not run")
	}
	files, _ := filepath.Glob(filepath.Join(tmp, "*"))
	for _, f := range files {
		if b, _ := os.ReadFile(f); bytes.Contains(b, []byte(marker)) {
			t.Errorf("%s holds the marker", f)
		}
	}
}

func TestFailedVacuumRunsAgainAtTheNextOpen(t *testing.T) {
	const marker = "residue-marker-retry-3c9e"
	p, key := oldStore(t, marker)
	p.vacuumHook = func() error { return errors.New("disk full") }
	if err := p.Open(); err == nil {
		t.Fatal("Open succeeded although VACUUM failed")
	}
	p.vacuumHook = nil
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if !p.vacuumed {
		t.Error("the Open after a failed VACUUM did not run it again")
	}
	assertGone(t, p, key, marker)
}
