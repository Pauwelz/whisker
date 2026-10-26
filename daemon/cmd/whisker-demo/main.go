// Command whisker-demo is a dev-only Daemon with made-up Chats, for screenshots.
// It uses the fake WhatsApp port and a fake keyring, and keeps its Profile in $XDG_RUNTIME_DIR/whisker-demo.
// It never touches the real Profile or the keyring. scripts/demo-daemon.sh builds it.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/profile"
	"github.com/Pauwelz/whisker/daemon/internal/profilelock"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// version is set at build time from manifest.json, so the Plugin accepts this binary.
var version = "dev"

// Fictional numbers from the +1 555-0100 range.
const (
	me    = "15550100@s.whatsapp.net"
	ana   = "15550101@s.whatsapp.net"
	ben   = "15550102@s.whatsapp.net"
	chloe = "15550103@s.whatsapp.net"
	dev   = "15550104@s.whatsapp.net"
	hike  = "120363000000000001@g.us"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	os.Exit(run())
}

func run() int {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		fmt.Fprintln(os.Stderr, "XDG_RUNTIME_DIR is not set")
		return contract.ExitError
	}
	// The same lock as the real Daemon, so the two never run together.
	lock, err := profilelock.Acquire(filepath.Join(runtimeDir, "whisker"), 5*time.Second)
	if errors.Is(err, profilelock.ErrInUse) {
		fmt.Fprintln(os.Stderr, "profile in use")
		return contract.ExitProfileInUse
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile lock failed:", err)
		return contract.ExitError
	}
	defer lock.Release()
	dir := filepath.Join(runtimeDir, "whisker-demo")
	// The fake keyring makes a new key each run, so the old stores cannot open.
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "demo reset failed:", err)
		return contract.ExitError
	}
	p := profile.New(filepath.Join(dir, "data"), keystore.New(keystore.NewFake(), clock.Real))
	defer p.Close()
	prefs := settings.New(filepath.Join(dir, "settings.json"))
	port := seed()
	return contract.Run(os.Stdin, os.Stdout, os.Stderr, p, prefs, func() (whatsapp.Port, error) { return port, nil }, &desktop.Fake{}) // the demo shows no real notification
}

// seed returns a linked fake port with the demo Chats queued as events.
func seed() *whatsapp.Fake {
	photo := landscape()
	f := whatsapp.NewFake()
	f.SetLinked(true)
	f.SetOwn("15550100")
	f.SetDownload(string(photo), nil)
	now := time.Now().Unix()
	ago := func(minutes int64) int64 { return now - minutes*60 }
	msg := func(id, sender string, minutes int64, text string) whatsapp.Message {
		return whatsapp.Message{ID: id, Sender: sender, FromMe: sender == me, Time: ago(minutes), Text: text, State: state(sender)}
	}

	photoMsg := msg("h3", ben, 41, "View from the top 🏔️")
	photoMsg.Media = &whatsapp.Media{Kind: media.Image, Mime: "image/jpeg", Size: int64(len(photo)), Thumbnail: photo}
	photoMsg.Reactions = []whatsapp.HistoryReaction{{Sender: ana, Emoji: "😍"}, {FromMe: true, Emoji: "🔥"}}
	reply := msg("h5", me, 12, "Count me in. I'll bring the coffee.")
	reply.Quote = &whatsapp.Quote{ID: "h4", Sender: chloe, Text: "Same trail next Saturday?"}

	f.Emit(whatsapp.Connected{})
	f.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{
		{JID: hike, Kind: whatsapp.KindGroup, Subject: "Weekend hike", LastActive: ago(12), Unread: true, Messages: []whatsapp.Message{
			msg("h1", ana, 95, "Made it to the hut! Who is still on the trail?"),
			msg("h2", me, 90, "Ten minutes behind you 🥾"),
			photoMsg,
			msg("h4", chloe, 20, "Same trail next Saturday?"),
			reply,
		}},
		{JID: ana, Kind: whatsapp.KindDirect, LastActive: ago(150), Pinned: true, Messages: []whatsapp.Message{
			msg("a1", ana, 160, "Did you get the tickets?"),
			msg("a2", me, 150, "Yes, two for Friday at 20:00 🎬"),
		}},
		{JID: ben, Kind: whatsapp.KindDirect, LastActive: ago(5), Unread: true, Messages: []whatsapp.Message{
			msg("b1", ben, 7, "Sending you the photos tonight"),
			msg("b2", ben, 5, "The sunset ones came out great"),
		}},
		{JID: dev, Kind: whatsapp.KindDirect, LastActive: ago(60 * 26), MutedUntil: -1, Messages: []whatsapp.Message{
			msg("d1", dev, 60*26, "Build is green, merging now ✅"),
		}},
		{JID: chloe, Kind: whatsapp.KindDirect, LastActive: ago(60 * 30), Messages: []whatsapp.Message{
			msg("c1", me, 60*30, "Thanks for the book tip!"),
			msg("c2", chloe, 60*30-3, "Let me know what you think 📚"),
		}},
		{JID: me, Kind: whatsapp.KindSelf, LastActive: ago(60 * 48), Messages: []whatsapp.Message{
			msg("s1", me, 60*48, "Groceries: oat milk, bread, tomatoes"),
		}},
	}})
	f.Emit(whatsapp.ContactBook{Names: []whatsapp.ContactName{
		{JID: ana, Name: "Ana Silva"}, {JID: ben, Name: "Ben Okafor"}, {JID: chloe, Name: "Chloé Martin"}, {JID: dev, Name: "Dev Team Bot"},
	}})
	return f
}

// state gives the user's own Messages a Read tick, as History Sync would.
func state(sender string) string {
	if sender == me {
		return whatsapp.StateRead
	}
	return ""
}

// landscape draws a small JPEG: a sky gradient over two mountain ridges.
func landscape() []byte {
	const w, h = 480, 320
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// top is the y of a ridge at x: peakY at the peak, the bottom edge at halfWidth from it.
	top := func(x, peak, halfWidth, peakY int) int {
		d := min(abs(x-peak), halfWidth)
		return peakY + (h-peakY)*d/halfWidth
	}
	for y := range h {
		for x := range w {
			c := color.RGBA{uint8(250 - y/3), uint8(170 + y/6), uint8(120 + y/3), 255} // dawn sky
			if y > top(x, 330, 240, 90) {
				c = color.RGBA{70, 90, 120, 255}
			}
			if y > top(x, 120, 260, 140) {
				c = color.RGBA{40, 55, 75, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		panic(err) // encoding to memory cannot fail for a valid image
	}
	return buf.Bytes()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
