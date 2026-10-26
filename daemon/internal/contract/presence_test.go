package contract

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/attention"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
)

type presencePortStub struct{ online []bool }

func (f *presencePortStub) SendPresence(online bool) error {
	f.online = append(f.online, online)
	return nil
}
func (f *presencePortStub) SubscribePresence(string) error { return nil }
func (f *presencePortStub) SendTyping(string, bool) error  { return nil }

// A settings file that breaks while the pop-out is open must still let Unavailable go out.
func TestPresenceUnavailableWhenSettingsBreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	port, attn := &presencePortStub{}, &attention.Attention{}
	p := newPresenter(port, settings.New(path), attn, io.Discard)
	attn.Set(true, "1@s.whatsapp.net")
	p.connected()
	os.WriteFile(path, []byte("{"), 0o600)
	attn.Set(false, "")
	p.sync()
	if want := []bool{true, false}; len(port.online) != 2 || port.online[0] != want[0] || port.online[1] != want[1] {
		t.Errorf("presence = %v, want %v", port.online, want)
	}
	p.sync() // nothing changed, and Online never goes out without a readable setting
	attn.Set(true, "1@s.whatsapp.net")
	p.sync()
	if len(port.online) != 2 {
		t.Errorf("presence = %v, want no further send", port.online)
	}
}
