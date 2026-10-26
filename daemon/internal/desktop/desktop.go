// Package desktop shows desktop notifications. It talks D-Bus itself, so message text and names
// never go into the arguments of a process, where other local users could read them.
package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/godbus/dbus/v5"
)

// Notification is what the user sees. A click on it opens the pop-out on Chat.
type Notification struct {
	Summary string
	Body    string // never parsed as options; Omarchy still renders it as markup (#87)
	Chat    string // the Chat id
}

// Notifier shows a notification and never blocks the caller.
// A failure is logged and the notification is dropped.
type Notifier interface {
	Notify(Notification)
}

const (
	appName   = "whisker"
	callLimit = 3 * time.Second // a slow notification server cannot hold a goroutine for long
	// The hint names and types are the ones of Omarchy's omarchy-notification-send.
	hintClick   = "omarchy-exec-argv" // string: the JSON argv that a click runs
	hintUrgency = "urgency"           // byte: 0 is low, as in omarchy-notification-send
)

// DBus shows notifications over the session bus.
type DBus struct{ LogOut io.Writer }

// Notify calls org.freedesktop.Notifications.Notify on its own goroutine.
func (d DBus) Notify(n Notification) {
	go func() {
		if err := call(n); err != nil {
			fmt.Fprintln(d.LogOut, "notification failed") // the error text could hold names
		}
	}()
}

func call(n Notification) error {
	conn, err := dbus.SessionBus() // shared by the process: do not close it
	if err != nil {
		return err
	}
	h, err := hints(n)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	return conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(ctx,
		"org.freedesktop.Notifications.Notify", 0,
		appName, uint32(0), "", n.Summary, n.Body, []string{}, h, int32(-1)).Err
}

// hints makes the click command: `omarchy-shell pauwelz.whisker openChat <chat>`, one argv word each.
func hints(n Notification) (map[string]dbus.Variant, error) {
	argv, err := json.Marshal([]string{"omarchy-shell", "pauwelz.whisker", "openChat", n.Chat})
	if err != nil {
		return nil, err
	}
	return map[string]dbus.Variant{hintUrgency: dbus.MakeVariant(byte(0)), hintClick: dbus.MakeVariant(string(argv))}, nil
}
