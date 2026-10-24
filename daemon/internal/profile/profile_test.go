package profile_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/profile"
)

// instantClock makes every keyring timeout fire at once.
type instantClock struct{}

func (instantClock) Now() time.Time { return time.Time{} }

func (instantClock) After(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Time{}
	return c
}

func newProfile(t *testing.T, fake *keystore.Fake) *profile.Profile {
	t.Helper()
	return profile.New(filepath.Join(t.TempDir(), "whisker"), keystore.New(fake, clock.Real))
}

func TestStoreFilesHoldNoPlaintext(t *testing.T) {
	p := newProfile(t, keystore.NewFake())
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat().ExecContext(context.Background(),
		"CREATE TABLE chat_marker (note TEXT); INSERT INTO chat_marker VALUES ('plaintext-marker-7f3a')"); err != nil {
		t.Fatal(err)
	}
	p.Close()

	for _, name := range []string{"device.db", "chat.db"} {
		data, err := os.ReadFile(filepath.Join(p.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, plain := range []string{"SQLite format 3", "whatsmeow_", "chat_marker", "plaintext-marker-7f3a"} {
			if bytes.Contains(data, []byte(plain)) {
				t.Errorf("%s holds plaintext %q", name, plain)
			}
		}
	}
}

func TestReopenWithTheSameKey(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if err := p.Open(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	p.Close()
}

func TestLostKeyDeletesTheUnreadableProfile(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	old, _ := os.ReadFile(filepath.Join(p.Dir, "device.db"))
	fake.Delete() // the key is lost

	if err := p.Open(); err != nil {
		t.Fatalf("Open after a lost key: %v", err)
	}
	p.Close()
	fresh, _ := os.ReadFile(filepath.Join(p.Dir, "device.db"))
	if bytes.Equal(old, fresh) {
		t.Error("the old stores are still there")
	}
}

// A key in the keyring that does not open the stores (wrong key) starts a fresh Profile.
func TestWrongKeyStartsAFreshProfile(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	wrong := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if err := fake.Set(wrong); err != nil {
		t.Fatal(err)
	}
	if err := p.Open(); err != nil {
		t.Fatalf("Open with a wrong key: %v", err)
	}
	defer p.Close()
	if got, _ := fake.Get(); got == wrong {
		t.Error("the wrong key is still in the keyring")
	}
	if _, err := p.Chat().ExecContext(context.Background(), "SELECT count(*) FROM chats"); err != nil {
		t.Errorf("fresh chat store unusable: %v", err)
	}
}

func TestUnavailableKeyringLeavesTheProfileAlone(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	fake.Fail(os.ErrPermission)
	p = profile.New(p.Dir, keystore.New(fake, instantClock{}))
	if err := p.Open(); err == nil {
		t.Fatal("Open must fail")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "device.db")); err != nil {
		t.Errorf("the Profile must stay: %v", err)
	}
}

// A chat store from a newer Daemon is not a wrong key: Open fails and keeps the Profile and its key.
func TestNewerChatStoreFailsOpenAndKeepsTheProfile(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat().ExecContext(context.Background(), "PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	p.Close()
	key, _ := fake.Get()
	if err := p.Open(); err == nil {
		t.Fatal("Open accepted a chat store from a newer Daemon")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "chat.db")); err != nil {
		t.Errorf("the Profile must stay: %v", err)
	}
	if got, _ := fake.Get(); got != key || got == "" {
		t.Error("the key must stay in the keyring")
	}
}

func TestDeleteRemovesTheProfileAndItsKey(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if err := p.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Dir); !os.IsNotExist(err) {
		t.Errorf("Profile directory still there: %v", err)
	}
	if _, err := fake.Get(); err == nil {
		t.Error("store key still in the keyring")
	}
}
