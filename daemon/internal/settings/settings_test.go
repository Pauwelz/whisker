package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/settings"
)

func TestDefaultsWithoutFile(t *testing.T) {
	got, err := settings.New(filepath.Join(t.TempDir(), "settings.json")).Get()
	want := settings.Settings{ReadReceipts: "onOpen", SendOnline: true, SendTyping: true, Notifications: true, AutoDownload: true}
	if err != nil || got != want {
		t.Errorf("Get = %+v, %v; want %+v", got, err, want)
	}
}

func TestSetSurvivesANewStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	mode, off := "off", false
	if _, err := settings.New(path).Set(settings.Update{ReadReceipts: &mode, SendTyping: &off, AutoDownload: &off}); err != nil {
		t.Fatal(err)
	}
	got, _ := settings.New(path).Get()
	want := settings.Settings{ReadReceipts: "off", SendOnline: true, SendTyping: false, Notifications: true, AutoDownload: false}
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

func TestBrokenFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{"), 0o600)
	if _, err := settings.New(path).Get(); err == nil {
		t.Error("Get on a broken file must fail")
	}
}
