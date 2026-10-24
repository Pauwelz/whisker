package whatsapp

import "sync"

// Fake is a scripted Port for tests. A test calls Emit to play WhatsApp events.
type Fake struct {
	events chan Event

	mu         sync.Mutex
	linked     bool
	pairCode   string
	pairErr    error
	connectErr error
	connects   int
	logoutErr  error
	logouts    int
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
