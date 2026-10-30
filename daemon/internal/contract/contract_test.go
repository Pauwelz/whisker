package contract_test

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/desktop"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
	"github.com/Pauwelz/whisker/daemon/internal/media"
	"github.com/Pauwelz/whisker/daemon/internal/profile"
	"github.com/Pauwelz/whisker/daemon/internal/settings"
	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

const fixtureGlob = "../../../contract/fixtures/*.jsonl"

type fixtureLine struct {
	Dir   string          `json:"dir"`
	Frame json.RawMessage `json:"frame"`
	State string          `json:"state"`

	// Fake WhatsApp lines (dir "whatsapp"): a start-up setting or an event, see docs/contract.md.
	Linked        *bool               `json:"linked"`
	PairCode      string              `json:"pairCode"`
	PairError     string              `json:"pairError"`
	Pictures      map[string]string   `json:"pictures"`    // dir "whatsapp": chat to "id|content" for chat.picture
	Lookups       map[string]string   `json:"lookups"`     // dir "whatsapp": digits to WhatsApp ID for contacts.lookup
	Own           []string            `json:"own"`         // dir "whatsapp": the user parts of the user's own IDs
	GroupAdmins   map[string][]string `json:"groupAdmins"` // dir "whatsapp": Group Chat to the admins that the live check finds
	GroupError    *string             `json:"groupError"`  // dir "whatsapp": "offline" fails the live admin check, "" lets it work again
	Phones        map[string]string   `json:"phones"`      // dir "whatsapp": LID to phone JID in the local LID store
	Hold          *bool               `json:"hold"`        // dir "whatsapp": true holds every send until false
	SendError     *string             `json:"sendError"`   // "" lets sends work again
	ReadError     *string             `json:"readError"`   // "" lets Read Receipts work again
	Receipts      []whatsapp.Read     `json:"receipts"`    // dir "read": the Read Receipts the fake port has sent so far
	Plays         []whatsapp.Read     `json:"plays"`       // dir "read": the Played receipts the fake port has sent so far
	Markers       []string            `json:"markers"`     // dir "read": the Chats whose Read Marker the fake port has cleared
	SelfReads     []whatsapp.Read     `json:"selfReads"`   // dir "read": the read-self receipts the fake port has sent so far
	IDs           []string            `json:"ids"`         // dir "sent" and "revoked": the Message IDs
	Edits         []string            `json:"edits"`       // dir "edited": "id:text" of each edit the fake port has sent so far
	Reactions     []string            `json:"reactions"`   // dir "reacted": "id:emoji" of each Reaction the fake port has sent so far
	Quotes        []string            `json:"quotes"`      // dir "quoted": "id:quotedID" of each reply the fake port has sent so far
	Pins          []string            `json:"pins"`        // dir "pinned": "chat:true" or "chat:false" of each pin write the fake port has accepted
	Unreads       []string            `json:"unreads"`     // dir "unread": "chat:true" or "chat:false" of each Read Marker write the fake port has accepted
	Deletes       []string            `json:"deletes"`     // dir "deletedForMe": "chat:id" of each delete-for-me write the fake port has accepted
	Mutes         []string            `json:"mutes"`       // dir "muted": "chat:until" of each mute write the fake port has accepted
	Event         json.RawMessage     `json:"event"`
	Requests      []string            `json:"requests"`      // dir "pictured": "chat:knownID" of each picture call that reached the fake port. Dir "historyRequested": "chat:anchorID:count" of each history request
	HistoryAnswer *int                `json:"historyAnswer"` // dir "whatsapp": the Messages in the answer to each history request, 0 for an empty batch
	HistoryTimer  bool                `json:"historyTimer"`  // dir "clock": fire the timeout of the open history request, queued behind the sweep timer

	Download      *string    `json:"download"`      // dir "whatsapp": the plain Media the next downloads return
	DownloadError *string    `json:"downloadError"` // dir "whatsapp": "" lets downloads work again
	Path          string     `json:"path"`          // dir "file"
	Content       string     `json:"content"`       // dir "file": the content the file must hold
	Missing       bool       `json:"missing"`       // dir "file": the file must not exist
	Remove        bool       `json:"remove"`        // dir "file": the test deletes the file
	Write         bool       `json:"write"`         // dir "file": the test writes the file with `content`
	Uploads       []string   `json:"uploads"`       // dir "uploaded": "id|kind|mime|name|caption|content" of each Media the fake port has sent
	Present       bool       `json:"present"`       // dir "file": the file must exist
	Now           *int64     `json:"now"`           // dir "clock" and "restart": the Daemon time in Unix seconds
	Timers        []int64    `json:"timers"`        // dir "sent": the Disappearing timer of each sent Message
	Online        []bool     `json:"online"`        // dir "presence": the availability calls so far
	Typing        []string   `json:"typing"`        // dir "presence": "chat:composing" or "chat:paused" calls so far
	Subscribed    []string   `json:"subscribed"`    // dir "presence": the Presence subscriptions so far
	Notified      []notified `json:"notified"`      // dir "notified": the desktop notifications so far
}

// notified is one desktop notification in a fixture: the Chat name and the body. The click token is random:
// $TOKEN[n] in an `in` frame is the token of the nth notification.
type notified struct{ Summary, Body string }

// runner runs the Daemon on pipes, with a fake keyring, a manual clock and a Profile in a temp dir.
type runner struct {
	t       *testing.T
	fake    *keystore.Fake
	clock   *clock.Manual
	expiry  *clock.Manual // the timers of the Disappearing Message sweep
	dir     string
	runtime string // $XDG_RUNTIME_DIR of the Daemon
	in      *io.PipeWriter
	frames  chan string
	done    chan int
	errOut  logBuffer
	started bool

	portMu   sync.Mutex
	portUsed bool
	port     *whatsapp.Fake // the port of the current Profile: Profile deletion makes a new one
	desktop  *desktop.Fake  // the notifications the Daemon showed
}

func newRunner(t *testing.T) *runner {
	t.Helper()
	oldClock := contract.Clock
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	r := &runner{runtime: runtime, t: t, fake: keystore.NewFake(), clock: clock.NewManual(), expiry: clock.NewManual(), dir: filepath.Join(t.TempDir(), "whisker"), port: whatsapp.NewFake(), desktop: &desktop.Fake{}}
	r.expiry.SetNow(1000) // fixed Message times for the fixtures
	contract.Clock = r.expiry
	t.Cleanup(func() { contract.Clock = oldClock })
	return r
}

func (r *runner) currentPort() *whatsapp.Fake {
	r.portMu.Lock()
	defer r.portMu.Unlock()
	return r.port
}

// newPort hands out the first fake, then a new one for each later Profile.
func (r *runner) newPort() (whatsapp.Port, error) {
	r.portMu.Lock()
	defer r.portMu.Unlock()
	if r.portUsed {
		r.port = whatsapp.NewFake()
	}
	r.portUsed = true
	return r.port, nil
}

func (r *runner) start() {
	if r.started {
		return
	}
	r.started = true
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	r.in = inW
	r.frames = make(chan string, 64)
	r.done = make(chan int, 1)
	p := profile.New(r.dir, keystore.New(r.fake, r.clock))
	go func() {
		r.done <- contract.Run(inR, outW, &r.errOut, p, settings.New(filepath.Join(r.dir, "..", "settings.json")), r.newPort, r.desktop)
		p.Close()
		outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			r.frames <- sc.Text()
		}
		close(r.frames)
	}()
}

// send writes one raw line to the Daemon.
func (r *runner) send(line string) {
	r.start()
	if _, err := io.WriteString(r.in, line+"\n"); err != nil {
		r.t.Fatal(err)
	}
}

// logBuffer is the log of a running Daemon. A test may read it while the Daemon writes.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

// waitLog waits until the log holds text: a result that sends no frame shows only there.
func (r *runner) waitLog(text string) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(r.errOut.String(), text) {
		if time.Now().After(deadline) {
			r.t.Fatalf("logs = %q, want %q", r.errOut.String(), text)
		}
		time.Sleep(time.Millisecond)
	}
}

// finish closes stdin and returns the exit code, the frames the Daemon still sent, and its logs.
func (r *runner) finish() (int, []string, string) {
	r.start()
	r.in.Close()
	code := <-r.done
	var rest []string
	for f := range r.frames {
		rest = append(rest, f)
	}
	return code, rest, r.errOut.String()
}

func (r *runner) nextFrame() string {
	r.t.Helper()
	select {
	case f, ok := <-r.frames:
		if !ok {
			r.t.Fatal("the Daemon stopped before the expected frame")
		}
		return f
	case <-time.After(5 * time.Second):
		r.t.Fatal("timeout waiting for a frame")
	}
	return ""
}

func TestFixtures(t *testing.T) {
	paths, err := filepath.Glob(fixtureGlob)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r := newRunner(t)
			sc := bufio.NewScanner(bytes.NewReader(raw))
			for sc.Scan() {
				var line fixtureLine
				if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
					t.Fatalf("bad fixture line %q: %v", sc.Text(), err)
				}
				switch line.Dir {
				case "in":
					var text string
					if json.Unmarshal(line.Frame, &text) != nil {
						text = string(line.Frame) // an object frame is sent as it is
					}
					text = strings.ReplaceAll(text, "$RUNTIME", r.runtime)
					r.send(r.expandTokens(text))
				case "out":
					r.start()
					var want, got any
					if err := json.Unmarshal([]byte(r.expand(string(bytes.ReplaceAll(line.Frame, []byte("$RUNTIME"), []byte(r.runtime))))), &want); err != nil {
						t.Fatal(err)
					}
					text := r.nextFrame()
					if err := json.Unmarshal([]byte(text), &got); err != nil {
						t.Fatalf("stdout line is not JSON: %q", text)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("frame = %v, want %v", got, want)
					}
				case "keyring":
					r.setKeyring(line.State)
				case "profile":
					r.setProfile(line.State)
				case "whatsapp":
					r.playWhatsApp(line)
				case "sent":
					if got := r.currentPort().Sent(); !reflect.DeepEqual(got, append([]string{}, line.IDs...)) {
						t.Errorf("sent IDs = %v, want %v", got, line.IDs)
					}
					if line.Timers != nil {
						if got := r.currentPort().SentTimers(); !reflect.DeepEqual(got, line.Timers) {
							t.Errorf("sent timers = %v, want %v", got, line.Timers)
						}
					}
				case "read":
					f := r.currentPort()
					if got, want := mustJSON(f.Reads()), mustJSON(append([]whatsapp.Read{}, line.Receipts...)); got != want {
						t.Errorf("Read Receipts = %s, want %s", got, want)
					}
					if got, want := mustJSON(f.Plays()), mustJSON(append([]whatsapp.Read{}, line.Plays...)); got != want {
						t.Errorf("Played receipts = %s, want %s", got, want)
					}
					if got, want := mustJSON(f.SelfReads()), mustJSON(append([]whatsapp.Read{}, line.SelfReads...)); got != want {
						t.Errorf("read-self receipts = %s, want %s", got, want)
					}
					if got, want := mustJSON(f.ChatReads()), mustJSON(append([]string{}, line.Markers...)); got != want {
						t.Errorf("Read Markers cleared = %s, want %s", got, want)
					}
				case "pictured":
					if got := r.currentPort().PictureRequests(); !reflect.DeepEqual(got, append([]string{}, line.Requests...)) {
						t.Errorf("picture requests = %v, want %v", got, line.Requests)
					}
				case "historyRequested":
					if got := r.currentPort().HistoryRequests(); !reflect.DeepEqual(got, append([]string{}, line.Requests...)) {
						t.Errorf("history requests = %v, want %v", got, line.Requests)
					}
				case "edited":
					if got := r.currentPort().Edits(); !reflect.DeepEqual(got, append([]string{}, line.Edits...)) {
						t.Errorf("edits = %v, want %v", got, line.Edits)
					}
				case "revoked":
					if got := r.currentPort().Revokes(); !reflect.DeepEqual(got, append([]string{}, line.IDs...)) {
						t.Errorf("revokes = %v, want %v", got, line.IDs)
					}
				case "uploaded":
					if got := r.currentPort().Uploads(); !reflect.DeepEqual(got, append([]string{}, line.Uploads...)) {
						t.Errorf("uploads = %v, want %v", got, line.Uploads)
					}
				case "reacted":
					if got := r.currentPort().Reactions(); !reflect.DeepEqual(got, append([]string{}, line.Reactions...)) {
						t.Errorf("reactions = %v, want %v", got, line.Reactions)
					}
				case "quoted":
					if got := r.currentPort().Quotes(); !reflect.DeepEqual(got, append([]string{}, line.Quotes...)) {
						t.Errorf("quotes = %v, want %v", got, line.Quotes)
					}
				case "pinned":
					if got := r.currentPort().Pins(); !reflect.DeepEqual(got, append([]string{}, line.Pins...)) {
						t.Errorf("pins = %v, want %v", got, line.Pins)
					}
				case "unread":
					if got := r.currentPort().Unreads(); !reflect.DeepEqual(got, append([]string{}, line.Unreads...)) {
						t.Errorf("unreads = %v, want %v", got, line.Unreads)
					}
				case "deletedForMe":
					if got := r.currentPort().Deletes(); !reflect.DeepEqual(got, append([]string{}, line.Deletes...)) {
						t.Errorf("deletes = %v, want %v", got, line.Deletes)
					}
				case "muted":
					if got := r.currentPort().Mutes(); !reflect.DeepEqual(got, append([]string{}, line.Mutes...)) {
						t.Errorf("mutes = %v, want %v", got, line.Mutes)
					}
				case "presence":
					f := r.currentPort()
					if got, want := mustJSON(f.OnlineCalls()), mustJSON(append([]bool{}, line.Online...)); got != want {
						t.Errorf("availability calls = %s, want %s", got, want)
					}
					if got, want := mustJSON(f.TypingCalls()), mustJSON(append([]string{}, line.Typing...)); got != want {
						t.Errorf("typing calls = %s, want %s", got, want)
					}
					if got, want := mustJSON(f.Subscribed()), mustJSON(append([]string{}, line.Subscribed...)); got != want {
						t.Errorf("subscriptions = %s, want %s", got, want)
					}
				case "notified":
					got := []notified{}
					for _, c := range r.desktop.Calls() {
						got = append(got, notified{c.Summary, c.Body})
					}
					if want := append([]notified{}, line.Notified...); !reflect.DeepEqual(got, want) {
						t.Errorf("notifications = %v, want %v", got, want)
					}
				case "file":
					r.checkFile(line)
				case "clock":
					r.start()
					if line.HistoryTimer {
						r.expiry.Fire() // the sweep timer, set first
						r.expiry.Fire()
					} else if line.Now != nil { // the Disappearing Message sweep
						r.expiry.SetNow(*line.Now)
						r.expiry.Fire()
					} else {
						r.clock.Fire()
					}
				case "restart":
					r.restart(t, line.Now)
				}
				// "view" lines belong to the Plugin tests.
			}
			code, rest, _ := r.finish()
			if code != contract.ExitOK {
				t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
			}
			if len(rest) > 0 {
				t.Errorf("extra frames: %v", rest)
			}
		})
	}
}

// restart stops the Daemon like a closed stdin and makes it ready to start again on the same Profile.
// The Daemon time can move on while it is off.
func (r *runner) restart(t *testing.T, now *int64) {
	t.Helper()
	code, rest, _ := r.finish()
	if code != contract.ExitOK || len(rest) > 0 {
		t.Errorf("before the restart: exit code %d, extra frames %v", code, rest)
	}
	r.started = false
	prev := r.expiry.Now().Unix()
	r.expiry = clock.NewManual()
	r.expiry.SetNow(prev)
	contract.Clock = r.expiry
	r.portMu.Lock()
	r.port, r.portUsed = whatsapp.NewFake(), false
	r.portMu.Unlock()
	if now != nil {
		r.expiry.SetNow(*now)
	}
}

var mediaToken = regexp.MustCompile(`\$MEDIA\[([^|\]]+)\|([^\]]+)\]`)

// expandTokens turns $TOKEN[n] into the click token of the nth notification.
func (r *runner) expandTokens(text string) string {
	for i, n := range r.desktop.Calls() {
		text = strings.ReplaceAll(text, fmt.Sprintf("$TOKEN[%d]", i), n.Token)
	}
	return text
}

// expand turns $MEDIA[chat|id] into the hashed folder and file name of that Media. It computes the names itself, as a check on the store.
func (r *runner) expand(text string) string {
	if !strings.Contains(text, "$MEDIA[") {
		return text // the fake keyring blocks while it is locked
	}
	value, err := r.fake.Get()
	if err != nil {
		return text
	}
	key, _ := base64.StdEncoding.DecodeString(value)
	hash := func(parts ...string) string {
		h := hmac.New(sha256.New, media.NameKey(key))
		h.Write([]byte(strings.Join(parts, "\x00")))
		return hex.EncodeToString(h.Sum(nil))
	}
	return mediaToken.ReplaceAllStringFunc(text, func(m string) string {
		p := mediaToken.FindStringSubmatch(m)
		return hash("chat", p[1]) + "/" + hash("file", p[1], p[2])
	})
}

// checkFile checks a plain Media copy in the runtime folder: its content, or that it is gone.
func (r *runner) checkFile(line fixtureLine) {
	path := r.expand(strings.ReplaceAll(strings.ReplaceAll(line.Path, "$RUNTIME", r.runtime), "$PROFILE", r.dir))
	if line.Remove {
		if err := os.Remove(path); err != nil {
			r.t.Fatal(err)
		}
		return
	}
	if line.Write {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(line.Content), 0o600); err != nil {
			r.t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	switch {
	case line.Present && err != nil:
		r.t.Errorf("file %s is missing: %v", line.Path, err)
	case line.Present:
	case line.Missing && err == nil:
		r.t.Errorf("a plain Media copy stayed behind: %s", line.Path)
	case !line.Missing && (err != nil || string(got) != line.Content):
		r.t.Errorf("file %s = %q (%v), want %q", line.Path, got, err, line.Content)
	}
}

func (r *runner) setKeyring(state string) {
	switch state {
	case "missing":
		r.fake.Fail(errors.New("no secret service"))
	case "ok":
		r.fake.Fail(nil)
	case "plain":
		contract.KeyringPlain = func() bool { return true }
		r.t.Cleanup(func() { contract.KeyringPlain = func() bool { return false } })
	case "locked":
		r.fake.Lock()
	case "unlocked":
		r.fake.Unlock()
	default:
		r.t.Fatalf("unknown keyring state %q", state)
	}
}

// playWhatsApp scripts the fake WhatsApp port: start-up settings, then events.
func (r *runner) playWhatsApp(line fixtureLine) {
	f := r.currentPort()
	switch {
	case line.Linked != nil:
		f.SetLinked(*line.Linked)
	case line.PairCode != "":
		f.SetPairCode(line.PairCode, nil)
	case line.Lookups != nil:
		f.SetLookups(line.Lookups)
	case line.Pictures != nil:
		f.SetPictures(line.Pictures)
	case line.HistoryAnswer != nil:
		f.SetHistoryAnswer(*line.HistoryAnswer)
	case line.Phones != nil:
		f.SetPhones(line.Phones)
	case line.GroupAdmins != nil:
		f.SetGroupAdmins(line.GroupAdmins)
	case line.GroupError != nil:
		if *line.GroupError == "" {
			f.SetGroupError(nil)
		} else {
			f.SetGroupError(whatsapp.ErrOffline)
		}
	case line.Own != nil:
		f.SetOwn(line.Own...)
	case line.Hold != nil:
		f.SetHold(*line.Hold)
	case line.SendError != nil:
		if *line.SendError == "" {
			f.SetSendError(nil)
		} else if *line.SendError == "offline" {
			f.SetSendError(whatsapp.ErrOffline)
		} else {
			f.SetSendError(errors.New(*line.SendError))
		}
	case line.Download != nil:
		f.SetDownload(*line.Download, nil)
	case line.DownloadError != nil:
		if *line.DownloadError == "" {
			f.SetDownload("", nil)
		} else {
			f.SetDownload("", errors.New(*line.DownloadError))
		}
	case line.ReadError != nil:
		if *line.ReadError == "" {
			f.SetReadError(nil)
		} else {
			f.SetReadError(whatsapp.ErrOffline)
		}
	case line.PairError == "invalid-phone":
		f.SetPairCode("", whatsapp.ErrInvalidPhone)
	case line.PairError != "":
		f.SetPairCode("", errors.New(line.PairError))
	case line.Event != nil:
		r.start()
		var e struct{ Kind, Code, Reason string }
		if err := json.Unmarshal(line.Event, &e); err != nil {
			r.t.Fatal(err)
		}
		// The kinds historySync to unarchiveChats carry the JSON fields of their whatsapp type.
		switch e.Kind {
		case "historySync":
			emitJSON[whatsapp.HistorySync](r.t, f, line.Event)
		case "pushName":
			emitJSON[whatsapp.PushName](r.t, f, line.Event)
		case "contactName":
			emitJSON[whatsapp.ContactName](r.t, f, line.Event)
		case "contactBook":
			emitJSON[whatsapp.ContactBook](r.t, f, line.Event)
		case "pin":
			emitJSON[whatsapp.Pin](r.t, f, line.Event)
		case "archive":
			emitJSON[whatsapp.Archive](r.t, f, line.Event)
		case "unarchiveChats":
			emitJSON[whatsapp.UnarchiveChats](r.t, f, line.Event)
		case "mute":
			emitJSON[whatsapp.Mute](r.t, f, line.Event)
		case "readMarker":
			emitJSON[whatsapp.ReadMarker](r.t, f, line.Event)
		case "newMessage":
			emitJSON[whatsapp.NewMessage](r.t, f, line.Event)
		case "ownDevices":
			emitJSON[whatsapp.OwnDevices](r.t, f, line.Event)
		case "receipt":
			emitJSON[whatsapp.Receipt](r.t, f, line.Event)
		case "readElsewhere":
			emitJSON[whatsapp.ReadElsewhere](r.t, f, line.Event)
		case "messageEdited":
			emitJSON[whatsapp.MessageEdited](r.t, f, line.Event)
		case "messageRevoked":
			emitJSON[whatsapp.MessageRevoked](r.t, f, line.Event)
		case "deletedForMe":
			emitJSON[whatsapp.DeletedForMe](r.t, f, line.Event)
		case "chatCleared":
			emitJSON[whatsapp.ChatCleared](r.t, f, line.Event)
		case "chatDeleted":
			emitJSON[whatsapp.ChatDeleted](r.t, f, line.Event)
		case "groupSubject":
			emitJSON[whatsapp.GroupSubject](r.t, f, line.Event)
		case "chatTimer":
			emitJSON[whatsapp.ChatTimer](r.t, f, line.Event)
		case "chatAlias":
			emitJSON[whatsapp.ChatAlias](r.t, f, line.Event)
		case "reaction":
			emitJSON[whatsapp.Reaction](r.t, f, line.Event)
		case "pictureChanged":
			emitJSON[whatsapp.PictureChanged](r.t, f, line.Event)
		case "presence":
			emitJSON[whatsapp.Presence](r.t, f, line.Event)
		case "chatPresence":
			emitJSON[whatsapp.ChatPresence](r.t, f, line.Event)
		case "connected":
			f.Emit(whatsapp.Connected{})
		case "disconnected":
			f.Emit(whatsapp.Disconnected{Reason: e.Reason})
		case "qr":
			f.Emit(whatsapp.QRCode{Code: e.Code})
		case "pairSuccess":
			f.Emit(whatsapp.PairSuccess{})
		case "linkFailed":
			f.Emit(whatsapp.LinkFailed{Reason: e.Reason})
		case "loggedOut":
			f.Emit(whatsapp.LoggedOut{})
		default:
			r.t.Fatalf("unknown whatsapp event kind %q", e.Kind)
		}
	default:
		r.t.Fatal("empty whatsapp line")
	}
}

func (r *runner) setProfile(state string) {
	if state != "unreadable" || r.started {
		r.t.Fatalf("profile state %q must be a start-up line, and one of: unreadable", state)
	}
	// Stores from an earlier run whose key is gone.
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "device.db"), []byte("unreadable"), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// A failed connect of a linked Profile tells the Plugin, then connects again after ConnectRetryDelay.
func TestConnectFailureRetries(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetConnectError(errors.New("no network"))
	r.start()
	for _, want := range []string{"ready", "linked", "disconnected"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.port.SetConnectError(nil)
	r.expiry.Fire() // the sweep timer, set first
	r.expiry.Fire() // the connect retry
	deadline := time.Now().Add(5 * time.Second)
	for r.port.Connects() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.port.Connects() < 2 {
		t.Errorf("Connect calls = %d, want a retry", r.port.Connects())
	}
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
}

// A send that finds no connection stays pending, even when a reconnect comes before the send reports back.
func TestSendOfflineErrorKeepsTheMessagePending(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	for _, want := range []string{"ready", "linked"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.port.Emit(whatsapp.HistorySync{Chats: []whatsapp.HistoryChat{{JID: "1@s.whatsapp.net", LastActive: 1}}})
	r.nextFrame() // chatsChanged
	r.port.Emit(whatsapp.Connected{})
	r.nextFrame() // connected
	r.port.SetSendError(whatsapp.ErrOffline)
	r.send(`{"jsonrpc":"2.0","id":1,"method":"chat.send","params":{"chat":"1@s.whatsapp.net","text":"hi"}}`)
	r.nextFrame() // the pending Message
	for r.port.SendAttempts() < 1 {
		time.Sleep(time.Millisecond)
	}
	r.port.SetSendError(nil)
	r.port.Emit(whatsapp.Connected{})
	for _, want := range []string{"connected", `"state":"sent"`} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if got := r.port.Sent(); len(got) != 1 {
		t.Errorf("sent = %v, want one send", got)
	}
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
}

// Presence fails until whatsmeow knows the push name. The push name event sends it again.
func TestPresenceIsSentWhenThePushNameArrives(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetPresenceError(whatsapp.ErrOffline)
	r.start()
	for _, want := range []string{"ready", "linked"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.port.Emit(whatsapp.Connected{})
	r.nextFrame() // connected
	r.port.SetPresenceError(nil)
	r.port.Emit(whatsapp.OwnPushName{})
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
	if got := r.port.OnlineCalls(); len(got) != 1 || got[0] {
		t.Errorf("presence calls = %v, want one unavailable after the push name", got)
	}
}

// run feeds input to a Daemon with a working keyring and returns its exit code, stdout and stderr.
func run(t *testing.T, input string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	p := profile.New(filepath.Join(t.TempDir(), "whisker"), keystore.New(keystore.NewFake(), clock.NewManual()))
	code := contract.Run(strings.NewReader(input), &out, &errOut, p, settings.New(filepath.Join(t.TempDir(), "settings.json")), func() (whatsapp.Port, error) { return whatsapp.NewFake(), nil }, &desktop.Fake{})
	p.Close()
	return code, out.String(), errOut.String()
}

func TestOversizedLineExitsWithDistinctCode(t *testing.T) {
	line := strings.Repeat("a", contract.MaxLineBytes+1) + "\n"
	code, stdout, stderr := run(t, line)
	if code != contract.ExitOversizedLine {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOversizedLine)
	}
	if strings.Count(stdout, "\n") != 1 { // only the ready frame
		t.Errorf("stdout = %q, want only the ready frame", stdout)
	}
	if !strings.Contains(stderr, "line too long") {
		t.Errorf("stderr = %q, want an error", stderr)
	}
	if strings.Contains(stderr, "aaaa") {
		t.Error("stderr must not echo the line")
	}
}

func TestContractDocListsExitCodes(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/contract.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{contract.ExitOK, contract.ExitError, contract.ExitOversizedLine, contract.ExitProfileInUse} {
		if !strings.Contains(string(doc), fmt.Sprintf("| %d |", code)) {
			t.Errorf("docs/contract.md has no row for exit code %d", code)
		}
	}
}

func TestUnlinkFromThePhoneDeletesTheProfile(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	for _, want := range []string{"ready", "linked"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	r.currentPort().Emit(whatsapp.LoggedOut{})
	for _, want := range []string{"unlinked", "ready"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if calls := r.fake.Calls(); !strings.Contains(calls, "delete") {
		t.Errorf("keyring calls = %q, want the store key deleted", calls)
	}
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
}

func TestLinkRetryConnectsAgain(t *testing.T) {
	r := newRunner(t)
	r.start()
	r.nextFrame() // ready
	r.send(`{"jsonrpc":"2.0","id":1,"method":"link.retry"}`)
	r.nextFrame() // result
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
	if got := r.currentPort().Connects(); got != 2 {
		t.Errorf("Connect calls = %d, want 2 (start and retry)", got)
	}
}

func TestContractDocListsLinkFailureReasons(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/contract.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{whatsapp.ReasonTimeout, whatsapp.ReasonClientOutdated, whatsapp.ReasonScannedWithoutMultidevice,
		whatsapp.ReasonPairError, whatsapp.ReasonConnectFailed, whatsapp.ReasonUnexpected} {
		if !strings.Contains(string(doc), "| `"+reason+"` |") {
			t.Errorf("docs/contract.md has no row for link failure reason %s", reason)
		}
	}
}

func TestContractDocListsDisconnectReasons(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/contract.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{whatsapp.DisconnectReplaced, whatsapp.DisconnectTemporaryBan, whatsapp.DisconnectConnectFailure} {
		if !strings.Contains(string(doc), "| `"+reason+"` |") {
			t.Errorf("docs/contract.md has no row for disconnect reason %s", reason)
		}
	}
}

// emitJSON plays an event whose fields are the JSON keys of its whatsapp type.
func emitJSON[T whatsapp.Event](t *testing.T, f *whatsapp.Fake, raw json.RawMessage) {
	t.Helper()
	var ev T
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	f.Emit(ev)
}

func TestLogoutKeepsProfileSettingsAndDeletesTheProfile(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(`{"jsonrpc":"2.0","id":1,"method":"settings.set","params":{"notifications":false}}`)
	r.nextFrame()
	r.send(`{"jsonrpc":"2.0","id":2,"method":"profile.logout"}`)
	for _, want := range []string{`"result"`, "unlinked", "ready"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if !strings.Contains(r.fake.Calls(), "delete") {
		t.Errorf("keyring calls = %q, want the store key deleted", r.fake.Calls())
	}
	r.send(`{"jsonrpc":"2.0","id":3,"method":"settings.get"}`)
	if f := r.nextFrame(); !strings.Contains(f, `"notifications":false`) {
		t.Errorf("frame = %s, want the setting kept", f)
	}
	if code, _, _ := r.finish(); code != contract.ExitOK {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
	}
}

// Offline, the remote logout fails. The Profile and its key go anyway, so the user is never stuck.
func TestFailedRemoteLogoutStillDeletesTheProfile(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetLogoutError(errors.New("offline"))
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(`{"jsonrpc":"2.0","id":1,"method":"profile.logout"}`)
	for _, want := range []string{`"result"`, "unlinked", "ready"} {
		if f := r.nextFrame(); !strings.Contains(f, want) {
			t.Fatalf("frame = %s, want %s", f, want)
		}
	}
	if !strings.Contains(r.fake.Calls(), "delete") {
		t.Errorf("keyring calls = %q, want the store key deleted", r.fake.Calls())
	}
	if _, _, logs := r.finish(); !strings.Contains(logs, "remote logout failed") {
		t.Errorf("log = %q, want the remote logout failure", logs)
	}
}

func TestLogoutNeedsALinkedProfile(t *testing.T) {
	r := newRunner(t)
	r.start()
	r.nextFrame() // ready
	r.send(`{"jsonrpc":"2.0","id":1,"method":"profile.logout"}`)
	if f := r.nextFrame(); !strings.Contains(f, "-32601") {
		t.Errorf("frame = %s, want method not found", f)
	}
	r.finish()
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Only a Log out from Settings removes a Profile that Open set aside. An unlink from the phone keeps it.
func TestOnlyUserLogoutRemovesTheSetAsideProfile(t *testing.T) {
	for _, byUser := range []bool{false, true} {
		r := newRunner(t)
		aside := r.dir + ".unreadable-20260101T000000.000000"
		if err := os.MkdirAll(aside, 0o700); err != nil {
			t.Fatal(err)
		}
		r.port.SetLinked(true)
		r.start()
		r.nextFrame() // ready
		r.nextFrame() // linked
		if byUser {
			r.send(`{"jsonrpc":"2.0","id":1,"method":"profile.logout"}`)
			r.nextFrame() // result
		} else {
			r.currentPort().Emit(whatsapp.LoggedOut{})
		}
		for _, want := range []string{"unlinked", "ready"} {
			if f := r.nextFrame(); !strings.Contains(f, want) {
				t.Fatalf("frame = %s, want %s", f, want)
			}
		}
		if _, err := os.Stat(aside); (err == nil) == byUser {
			t.Errorf("by user %v: set-aside folder exists = %v", byUser, err == nil)
		}
		r.finish()
	}
}
