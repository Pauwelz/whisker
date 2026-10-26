package whatsapp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// A group member can send a short skmsg. libsignal logs its raw bytes. They must not reach fd 1,
// because the Plugin reads fd 1 as frames.
func TestShortGroupSkmsgWritesNothingToStdout(t *testing.T) {
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:"+filepath.Join(t.TempDir(), "device.db")+"?_pragma=foreign_keys(1)", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(container)
	if err != nil {
		t.Fatal(err)
	}

	c.cli.Store.ID = &types.JID{User: "3160", Server: types.DefaultUserServer} // parsing needs a linked device

	saved, err := syscall.Dup(syscall.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Dup3(int(w.Fd()), syscall.Stdout, 0); err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	node := &waBinary.Node{
		Tag: "message",
		Attrs: waBinary.Attrs{
			"id": "3EB0TEST", "t": "1700000000", "type": "text",
			"from":        types.JID{User: "120363000000000000", Server: types.GroupServer},
			"participant": types.JID{User: "3161", Server: types.HiddenUserServer},
		},
		Content: []waBinary.Node{{
			Tag:     "enc",
			Attrs:   waBinary.Attrs{"type": "skmsg", "v": "2"},
			Content: []byte(`{"jsonrpc":"2.0","method":"x"}`),
		}},
	}
	c.cli.DangerousInternals().HandleEncryptedMessage(context.Background(), node)

	syscall.Dup3(saved, syscall.Stdout, 0)
	syscall.Close(saved)
	w.Close()
	if got, _ := io.ReadAll(r); len(got) != 0 {
		t.Errorf("fd 1 got %q, want nothing", got)
	}
}
