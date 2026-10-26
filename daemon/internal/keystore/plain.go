package keystore

import (
	"bytes"
	"io"
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
		if n := strings.TrimSpace(string(b)); n != "" {
			name = n
		}
	}
	if name != filepath.Base(name) { // a name with a path could point outside the keyrings folder
		return false
	}
	f, err := os.Open(filepath.Join(dir, name+".keyring"))
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(plainHeader))
	n, _ := io.ReadFull(f, head)
	return bytes.Equal(head[:n], plainHeader)
}
