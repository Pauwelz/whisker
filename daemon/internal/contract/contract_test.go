package contract_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
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
	Linked    *bool           `json:"linked"`
	PairCode  string          `json:"pairCode"`
	PairError string          `json:"pairError"`
	SendError *string         `json:"sendError"` // "" lets sends work again
	IDs       []string        `json:"ids"`       // dir "sent" and "revoked": the Message IDs
	Edits     []string        `json:"edits"`     // dir "edited": "id:text" of each edit the fake port has sent so far
	Event     json.RawMessage `json:"event"`

	Download      *string `json:"download"`      // dir "whatsapp": the plain Media the next downloads return
	DownloadError *string `json:"downloadError"` // dir "whatsapp": "" lets downloads work again
	Path          string  `json:"path"`          // dir "file"
	Content       string  `json:"content"`       // dir "file": the content the file must hold
	Missing       bool    `json:"missing"`       // dir "file": the file must not exist
}

// runner runs the Daemon on pipes, with a fake keyring, a manual clock and a Profile in a temp dir.
type runner struct {
	t       *testing.T
	fake    *keystore.Fake
	clock   *keystore.ManualClock
	dir     string
	runtime string // $XDG_RUNTIME_DIR of the Daemon
	in      *io.PipeWriter
	frames  chan string
	done    chan int
	errOut  bytes.Buffer
	started bool

	portMu   sync.Mutex
	portUsed bool
	port     *whatsapp.Fake // the port of the current Profile: Profile deletion makes a new one
}

func newRunner(t *testing.T) *runner {
	t.Helper()
	oldNow := contract.Now
	contract.Now = func() time.Time { return time.Unix(1000, 0) } // fixed Message times for the fixtures
	t.Cleanup(func() { contract.Now = oldNow })
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	return &runner{runtime: runtime, t: t, fake: keystore.NewFake(), clock: keystore.NewManualClock(), dir: filepath.Join(t.TempDir(), "whisker"), port: whatsapp.NewFake()}
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
		r.done <- contract.Run(inR, outW, &r.errOut, p, settings.New(filepath.Join(r.dir, "..", "settings.json")), r.newPort)
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
					r.send(text)
				case "out":
					r.start()
					var want, got any
					if err := json.Unmarshal(bytes.ReplaceAll(line.Frame, []byte("$RUNTIME"), []byte(r.runtime)), &want); err != nil {
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
				case "edited":
					if got := r.currentPort().Edits(); !reflect.DeepEqual(got, append([]string{}, line.Edits...)) {
						t.Errorf("edits = %v, want %v", got, line.Edits)
					}
				case "revoked":
					if got := r.currentPort().Revokes(); !reflect.DeepEqual(got, append([]string{}, line.IDs...)) {
						t.Errorf("revokes = %v, want %v", got, line.IDs)
					}
				case "file":
					r.checkFile(line)
				case "clock":
					r.start()
					r.clock.Fire()
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

// checkFile checks a plain Media copy in the runtime folder: its content, or that it is gone.
func (r *runner) checkFile(line fixtureLine) {
	path := strings.ReplaceAll(line.Path, "$RUNTIME", r.runtime)
	got, err := os.ReadFile(path)
	switch {
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
		// The kinds historySync to readMarker carry the JSON fields of their whatsapp type.
		switch e.Kind {
		case "historySync":
			emitJSON[whatsapp.HistorySync](r.t, f, line.Event)
		case "pushName":
			emitJSON[whatsapp.PushName](r.t, f, line.Event)
		case "contactName":
			emitJSON[whatsapp.ContactName](r.t, f, line.Event)
		case "pin":
			emitJSON[whatsapp.Pin](r.t, f, line.Event)
		case "mute":
			emitJSON[whatsapp.Mute](r.t, f, line.Event)
		case "readMarker":
			emitJSON[whatsapp.ReadMarker](r.t, f, line.Event)
		case "newMessage":
			emitJSON[whatsapp.NewMessage](r.t, f, line.Event)
		case "receipt":
			emitJSON[whatsapp.Receipt](r.t, f, line.Event)
		case "messageEdited":
			emitJSON[whatsapp.MessageEdited](r.t, f, line.Event)
		case "messageRevoked":
			emitJSON[whatsapp.MessageRevoked](r.t, f, line.Event)
		case "connected":
			f.Emit(whatsapp.Connected{})
		case "disconnected":
			f.Emit(whatsapp.Disconnected{})
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
	old := contract.ConnectRetryDelay
	contract.ConnectRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { contract.ConnectRetryDelay = old })
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

// run feeds input to a Daemon with a working keyring and returns its exit code, stdout and stderr.
func run(t *testing.T, input string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	p := profile.New(filepath.Join(t.TempDir(), "whisker"), keystore.New(keystore.NewFake(), keystore.NewManualClock()))
	code := contract.Run(strings.NewReader(input), &out, &errOut, p, settings.New(filepath.Join(t.TempDir(), "settings.json")), func() (whatsapp.Port, error) { return whatsapp.NewFake(), nil })
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

func TestFailedLogoutKeepsTheProfile(t *testing.T) {
	r := newRunner(t)
	r.port.SetLinked(true)
	r.port.SetLogoutError(errors.New("offline"))
	r.start()
	r.nextFrame() // ready
	r.nextFrame() // linked
	r.send(`{"jsonrpc":"2.0","id":1,"method":"profile.logout"}`)
	if f := r.nextFrame(); !strings.Contains(f, "-32005") {
		t.Fatalf("frame = %s, want error -32005", f)
	}
	if strings.Contains(r.fake.Calls(), "delete") {
		t.Error("a failed Log out must not delete the Profile")
	}
	r.send(`{"jsonrpc":"2.0","id":2,"method":"chats.list"}`) // still ready and linked
	if f := r.nextFrame(); !strings.Contains(f, `"chats"`) {
		t.Errorf("frame = %s, want a Chat list", f)
	}
	r.finish()
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
