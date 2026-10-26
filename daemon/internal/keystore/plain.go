package keystore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// plainHeader starts a gnome-keyring file that has no password. An encrypted one starts with "GnomeKeyring\n\r\0\n".
var plainHeader = []byte("[keyring]")

// PlainKeyring reports whether the default gnome-keyring collection under dataHome has no password.
// Then the file holds every secret in plain text. Any read error, or another provider, gives false.
func PlainKeyring(dataHome string) bool {
	dir := filepath.Join(dataHome, "keyrings")
	name := "login"
	if b, err := os.ReadFile(filepath.Join(dir, "default")); err == nil {
		name = strings.TrimSpace(string(b))
	}
	f, err := os.Open(filepath.Join(dir, filepath.Base(name)+".keyring"))
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(plainHeader))
	n, _ := f.Read(head)
	return bytes.Equal(head[:n], plainHeader)
}
