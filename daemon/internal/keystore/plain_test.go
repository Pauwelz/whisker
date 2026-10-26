package keystore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/keystore"
)

func writeKeyring(t *testing.T, home, file, content string) {
	t.Helper()
	dir := filepath.Join(home, "keyrings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPlainKeyring(t *testing.T) {
	const plain, encrypted = "[keyring]\ndisplay-name=Default\n", "GnomeKeyring\n\r\x00\n...."
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"plain default keyring", map[string]string{"default": "Default_keyring\n", "Default_keyring.keyring": plain}, true},
		{"encrypted default keyring", map[string]string{"default": "Default_keyring\n", "Default_keyring.keyring": encrypted}, false},
		{"no default file falls back to login", map[string]string{"login.keyring": plain}, true},
		{"the default file wins over login", map[string]string{"default": "other", "other.keyring": encrypted, "login.keyring": plain}, false},
		{"missing keyring file", map[string]string{"default": "Default_keyring"}, false},
		{"relative path in the name is not followed", map[string]string{"default": "../x", "x.keyring": plain}, false},
		{"absolute path in the name is not followed", map[string]string{"default": "/etc/x", "x.keyring": plain}, false},
		{"empty default file falls back to login", map[string]string{"default": " \n", "login.keyring": plain}, true},
		{"empty file", map[string]string{"login.keyring": ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			for file, content := range tt.files {
				writeKeyring(t, home, file, content)
			}
			if got := keystore.PlainKeyring(home); got != tt.want {
				t.Errorf("PlainKeyring = %v, want %v", got, tt.want)
			}
		})
	}
	if keystore.PlainKeyring(t.TempDir()) {
		t.Error("no keyrings folder must give false")
	}
}
