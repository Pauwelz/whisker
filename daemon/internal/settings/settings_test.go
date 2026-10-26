package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/settings"
)

func TestDefaultsWithoutFile(t *testing.T) {
	got, err := settings.New(filepath.Join(t.TempDir(), "settings.json")).Get()
	want := settings.Settings{ReadReceipts: "onOpen", SendOnline: true, SendTyping: true, Notifications: true, NotificationPreview: true, AutoDownload: true, AutoDownloadLimit: "5mb"}
	if err != nil || got != want {
		t.Errorf("Get = %+v, %v; want %+v", got, err, want)
	}
}

func TestSetSurvivesANewStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	mode, off := "off", false
	limit := "25mb"
	if _, err := settings.New(path).Set(settings.Update{ReadReceipts: &mode, SendTyping: &off, NotificationPreview: &off, AutoDownload: &off, AutoDownloadLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	got, _ := settings.New(path).Get()
	want := settings.Settings{ReadReceipts: "off", SendOnline: true, SendTyping: false, Notifications: true, NotificationPreview: false, AutoDownload: false, AutoDownloadLimit: "25mb"}
	if got != want {
		t.Errorf("Get = %+v, want %+v", got, want)
	}
}

func TestInvalidModeChangesNothing(t *testing.T) {
	s := settings.New(filepath.Join(t.TempDir(), "settings.json"))
	bad, off := "sometimes", false
	if _, err := s.Set(settings.Update{ReadReceipts: &bad, SendOnline: &off}); err != settings.ErrInvalid {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
	if got, _ := s.Get(); got != settings.Defaults() {
		t.Errorf("Get = %+v, want the defaults", got)
	}
}

func TestInvalidLimitChangesNothing(t *testing.T) {
	s := settings.New(filepath.Join(t.TempDir(), "settings.json"))
	bad := "2mb"
	if _, err := s.Set(settings.Update{AutoDownloadLimit: &bad}); err != settings.ErrInvalid {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
	if got, _ := s.Get(); got != settings.Defaults() {
		t.Errorf("Get = %+v, want the defaults", got)
	}
}

func TestAutoDownloadsFollowsTheSwitchAndTheLimit(t *testing.T) {
	const mb = 1 << 20
	on := settings.Defaults() // 5 MB
	cases := []struct {
		s    settings.Settings
		size int64
		want bool
	}{
		{on, 5 * mb, true},
		{on, 5*mb + 1, false},
		{on, 0, true}, // an unknown size downloads
		{settings.Settings{AutoDownload: true, AutoDownloadLimit: "any"}, 900 * mb, true},
		{settings.Settings{AutoDownload: true, AutoDownloadLimit: "1mb"}, 2 * mb, false},
		{settings.Settings{AutoDownload: false, AutoDownloadLimit: "any"}, 1, false},
	}
	for _, c := range cases {
		if got := c.s.AutoDownloads(c.size); got != c.want {
			t.Errorf("%+v AutoDownloads(%d) = %v, want %v", c.s, c.size, got, c.want)
		}
	}
}

func TestBrokenFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{"), 0o600)
	if _, err := settings.New(path).Get(); err == nil {
		t.Error("Get on a broken file must fail")
	}
}
