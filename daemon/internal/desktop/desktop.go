// Package desktop shows desktop notifications. It talks D-Bus itself, so message text and names
// never go into the arguments of a process, where other local users could read them.
package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Notification is what the user sees. A click on it opens the pop-out on the Chat of Token.
type Notification struct {
	Summary string
	Body    string // never parsed as options; the D-Bus call neutralises it, as Omarchy renders markup
	Token   string // opaque: it stands for the Chat, so the Chat id stays out of the click command
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
		appName, uint32(0), "", neutralise(n.Summary), neutralise(n.Body), []string{}, h, int32(-1)).Err
}

// neutraliser stops Omarchy from reading text as markup. It decodes entities up to 3 times,
// restores tags and links, and linkifies bare URLs. A zero-width space breaks each trigger.
var neutraliser = strings.NewReplacer("<", "<\u200b", "&", "&\u200b", "://", ":/\u200b/")

func neutralise(s string) string { return neutraliser.Replace(s) }

// hints makes the click command: `omarchy-shell pauwelz.whisker openChat <token>`, one argv word each.
func hints(n Notification) (map[string]dbus.Variant, error) {
	argv, err := json.Marshal([]string{"omarchy-shell", "pauwelz.whisker", "openChat", n.Token})
	if err != nil {
		return nil, err
	}
	return map[string]dbus.Variant{hintUrgency: dbus.MakeVariant(byte(0)), hintClick: dbus.MakeVariant(string(argv))}, nil
}
