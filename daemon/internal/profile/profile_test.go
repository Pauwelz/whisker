package profile_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
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

func TestLostKeyMovesTheUnreadableProfileAside(t *testing.T) {
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
	assertAside(t, p, old)
}

// assertAside checks that the old device store lives on in one <dir>.unreadable-* directory.
func assertAside(t *testing.T, p *profile.Profile, old []byte) {
	t.Helper()
	dirs, _ := filepath.Glob(p.Dir + ".unreadable-*")
	if len(dirs) != 1 {
		t.Fatalf("unreadable directories = %v, want one", dirs)
	}
	kept, err := os.ReadFile(filepath.Join(dirs[0], "device.db"))
	if err != nil || !bytes.Equal(kept, old) {
		t.Errorf("the old device store did not move aside: %v", err)
	}
	if !p.MovedAside() {
		t.Error("MovedAside = false")
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

// The old layout was <jid>/<id>. Open renames each file once.
func TestOpenRenamesTheMediaOfTheOldLayoutOnce(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	mk := bytes.Repeat([]byte{3}, 32)
	const jid = "31612345678@s.whatsapp.net"
	if err := p.Media().Put(jid, "m1", media.Image, mk, strings.NewReader("pic")); err != nil {
		t.Fatal(err)
	}
	// Put it back in the old layout.
	root := filepath.Join(p.Dir, "media")
	hashed, _ := filepath.Glob(filepath.Join(root, "*", "*"))
	os.MkdirAll(filepath.Join(root, jid), 0o700)
	if err := os.Rename(hashed[0], filepath.Join(root, jid, "m1")); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Dir(hashed[0]))
	p.Close()

	snapshot := func() string {
		var all []string
		filepath.WalkDir(root, func(path string, d os.DirEntry, _ error) error {
			rel, _ := filepath.Rel(root, path)
			all = append(all, rel)
			return nil
		})
		return strings.Join(all, "\n")
	}
	p2 := profile.New(p.Dir, keystore.New(fake, clock.Real))
	if err := p2.Open(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, jid)); err == nil {
		t.Error("the old folder stayed")
	}
	if !p2.Media().Has(jid, "m1") {
		t.Fatal("the file has no new name")
	}
	if path, err := p2.Media().Plain(jid, "m1", "", media.Image, mk); err != nil {
		t.Fatal(err)
	} else if got, _ := os.ReadFile(path); string(got) != "pic" {
		t.Errorf("file = %q", got)
	}
	before := snapshot()
	p2.Close()
	p3 := profile.New(p.Dir, keystore.New(fake, clock.Real))
	if err := p3.Open(); err != nil {
		t.Fatal(err)
	}
	defer p3.Close()
	if snapshot() != before {
		t.Error("a second Open changed the Media folder")
	}
}

func TestMediaBeforeOpenCanWipe(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	plain := filepath.Join(rt, "whisker", "media")
	os.MkdirAll(plain, 0o700)
	p := newProfile(t, keystore.NewFake())
	p.Media().Wipe()
	if _, err := os.Stat(plain); err == nil {
		t.Error("Wipe left the plain folder")
	}
}

// A '#', '?' or '%' in the data path must not end the path early, or the store opens without encryption.
func TestStoreStaysEncryptedInAPathWithURLCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b?c%25d e", "whisker")
	p := profile.New(dir, keystore.New(keystore.NewFake(), clock.Real))
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	data, err := os.ReadFile(filepath.Join(dir, "chat.db"))
	if err != nil {
		t.Fatalf("the store is not at the path: %v", err)
	}
	if bytes.Contains(data, []byte("SQLite format 3")) {
		t.Error("chat.db is not encrypted")
	}
}

// A key in the keyring that opens nothing moves the stores aside too.
func TestWrongKeyMovesTheProfileAside(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	old, _ := os.ReadFile(filepath.Join(p.Dir, "device.db"))
	fake.Set(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	assertAside(t, p, old)
}

// Delete keeps the set-aside Profiles. Log out removes them with RemoveUnreadable.
func TestRemoveUnreadableRemovesTheSetAsideProfiles(t *testing.T) {
	fake := keystore.NewFake()
	p := newProfile(t, fake)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	fake.Delete()
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if err := p.Delete(); err != nil {
		t.Fatal(err)
	}
	if !p.MovedAside() {
		t.Error("Delete must keep the unreadable Profiles")
	}
	if err := p.RemoveUnreadable(); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(p.Dir + "*"); len(left) != 0 || p.MovedAside() {
		t.Errorf("left after Delete: %v", left)
	}
}
