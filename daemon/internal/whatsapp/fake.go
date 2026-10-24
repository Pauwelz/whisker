package whatsapp

import (
	"fmt"
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
	attempts   int
	ids        int
	logoutErr  error
	logouts    int
	readErr    error
	reads      []Read
	chatReads  []string
	edits      []string
	revokes    []string
	quotes     []string
	reactions  []string
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

func (f *Fake) Send(_, id, _ string, quote *Quote) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, id)
	if quote != nil {
		f.quotes = append(f.quotes, id+":"+quote.ID)
	}
	return nil
}

// React fails with the send error, like Send. A good call is recorded as "id:emoji".
func (f *Fake) React(_, id, _, emoji string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.reactions = append(f.reactions, id+":"+emoji)
	return nil
}

// Quotes lists the sent replies as "id:quotedID", in order. Reactions lists the Reactions as "id:emoji".
func (f *Fake) Quotes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.quotes...)
}

func (f *Fake) Reactions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.reactions...)
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
