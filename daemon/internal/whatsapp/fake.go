package whatsapp

import (
	"fmt"
	"os"
	"sync"
)

// Fake is a scripted Port for tests. A test calls Emit to play WhatsApp events.
type Fake struct {
	events chan Event

	mu         sync.Mutex
	linked     bool
	pairCode   string
	pairErr    error
	connectErr error
	connects   int
	sendErr    error
	sent       []string
	timers     []int64
	attempts   int
	ids        int
	logoutErr  error
	logouts    int
	content    string
	dlErr      error
	downloads  int
	readErr    error
	reads      []Read
	chatReads  []string
	edits      []string
	revokes    []string
	online     []bool
	subscribed []string
	typing     []string
}

func NewFake() *Fake { return &Fake{events: make(chan Event, 64)} }

// SetLinked sets the state at start-up.
func (f *Fake) SetLinked(linked bool) { f.mu.Lock(); f.linked = linked; f.mu.Unlock() }

// SetPairCode scripts the answer of PairCode. A non-nil err wins.
func (f *Fake) SetPairCode(code string, err error) {
	f.mu.Lock()
	f.pairCode, f.pairErr = code, err
	f.mu.Unlock()
}

// SetConnectError makes Connect fail with err. Pass nil to stop failing.
func (f *Fake) SetConnectError(err error) { f.mu.Lock(); f.connectErr = err; f.mu.Unlock() }

// SetSendError makes Send fail with err. Pass nil to stop failing.
func (f *Fake) SetSendError(err error) { f.mu.Lock(); f.sendErr = err; f.mu.Unlock() }

// Sent lists the IDs that Send accepted, in order.
func (f *Fake) Sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

// SendAttempts counts the Send calls, failed or not.
func (f *Fake) SendAttempts() int { f.mu.Lock(); defer f.mu.Unlock(); return f.attempts }

func (f *Fake) NewMessageID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids++
	return fmt.Sprintf("fake-%d", f.ids)
}

// SentTimers lists the Disappearing timers that Send accepted, in order.
func (f *Fake) SentTimers() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64{}, f.timers...)
}

func (f *Fake) Send(_, id, _ string, timer int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, id)
	f.timers = append(f.timers, timer)
	return nil
}

// SetDownload scripts Download: it writes content to the file, or fails with err (a non-nil err wins).
func (f *Fake) SetDownload(content string, err error) {
	f.mu.Lock()
	f.content, f.dlErr = content, err
	f.mu.Unlock()
}

// Downloads counts the Download calls.
func (f *Fake) Downloads() int { f.mu.Lock(); defer f.mu.Unlock(); return f.downloads }

func (f *Fake) Download(_ Media, dst *os.File) error {
	f.mu.Lock()
	f.downloads++
	content, err := f.content, f.dlErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = dst.WriteString(content)
	return err
}

// Read is one MarkRead call that worked.
type Read struct {
	Chat   string   `json:"chat"`
	Sender string   `json:"sender"`
	IDs    []string `json:"ids"`
}

// SetReadError makes MarkRead fail with err. Pass nil to stop failing.
func (f *Fake) SetReadError(err error) { f.mu.Lock(); f.readErr = err; f.mu.Unlock() }

// Reads lists the MarkRead calls that worked, in order.
func (f *Fake) Reads() []Read {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Read{}, f.reads...)
}

// ChatReads lists the Chats of the MarkChatRead calls, in order.
func (f *Fake) ChatReads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.chatReads...)
}

func (f *Fake) MarkRead(chat, sender string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return f.readErr
	}
	f.reads = append(f.reads, Read{chat, sender, append([]string{}, ids...)})
	return nil
}

func (f *Fake) MarkChatRead(chat string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatReads = append(f.chatReads, chat)
	return nil
}

// Edit and Revoke fail with the send error, like Send. A good call is recorded.
func (f *Fake) Edit(_, id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.edits = append(f.edits, id+":"+text)
	return nil
}

func (f *Fake) Revoke(_, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.revokes = append(f.revokes, id)
	return nil
}

// Edits lists the edits that Edit accepted as "id:text", in order. Revokes lists the revoked IDs.
func (f *Fake) Edits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.edits...)
}

func (f *Fake) Revokes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.revokes...)
}

// SetLogoutError makes Logout fail with err. Pass nil to stop failing.
func (f *Fake) SetLogoutError(err error) { f.mu.Lock(); f.logoutErr = err; f.mu.Unlock() }

// Logouts counts the Logout calls.
func (f *Fake) Logouts() int { f.mu.Lock(); defer f.mu.Unlock(); return f.logouts }

func (f *Fake) Logout() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts++
	return f.logoutErr
}

// Connects counts the Connect calls.
func (f *Fake) Connects() int { f.mu.Lock(); defer f.mu.Unlock(); return f.connects }

// Emit plays an event. A PairSuccess also links the fake.
func (f *Fake) Emit(e Event) {
	if _, ok := e.(PairSuccess); ok {
		f.SetLinked(true)
	}
	f.events <- e
}

func (f *Fake) Linked() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.linked }

func (f *Fake) Connect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	return f.connectErr
}

func (f *Fake) PairCode(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pairCode, f.pairErr
}

func (f *Fake) Disconnect()          {}
func (f *Fake) Events() <-chan Event { return f.events }

// SendPresence, SubscribePresence and SendTyping record the call. Typing is recorded as "chat:composing" or "chat:paused".
func (f *Fake) SendPresence(online bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.online = append(f.online, online)
	return nil
}

func (f *Fake) SubscribePresence(jid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, jid)
	return nil
}

func (f *Fake) SendTyping(chat string, composing bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := ActivityPaused
	if composing {
		state = ActivityComposing
	}
	f.typing = append(f.typing, chat+":"+state)
	return nil
}

// OnlineCalls, Subscribed and TypingCalls list what the fake got, in order.
func (f *Fake) OnlineCalls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool{}, f.online...)
}

func (f *Fake) Subscribed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.subscribed...)
}

func (f *Fake) TypingCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.typing...)
}
