package keystore

import (
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Backend for tests. It never touches the real keyring.
type Fake struct {
	mu     sync.Mutex
	value  string
	has    bool
	err    error         // when set, every call fails with it
	locked chan struct{} // when set, every call blocks until Unlock
	calls  []string
}

func NewFake() *Fake { return &Fake{} }

// Fail makes every call return err. Pass nil to stop failing.
func (f *Fake) Fail(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }

// Lock makes every call block until Unlock, like a locked keyring with an open prompt.
func (f *Fake) Lock() { f.mu.Lock(); f.locked = make(chan struct{}); f.mu.Unlock() }

func (f *Fake) Unlock() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked != nil {
		close(f.locked)
		f.locked = nil
	}
}

// Calls lists the calls so far, for example "get,set".
func (f *Fake) Calls() string { f.mu.Lock(); defer f.mu.Unlock(); return strings.Join(f.calls, ",") }

// enter records the call and waits while the keyring is locked.
func (f *Fake) enter(name string) error {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	locked := f.locked
	f.mu.Unlock()
	if locked != nil {
		<-locked
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *Fake) Get() (string, error) {
	if err := f.enter("get"); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.has {
		return "", ErrNotFound
	}
	return f.value, nil
}

func (f *Fake) Set(value string) error {
	if err := f.enter("set"); err != nil {
		return err
	}
	f.mu.Lock()
	f.value, f.has = value, true
	f.mu.Unlock()
	return nil
}

func (f *Fake) Delete() error {
	if err := f.enter("delete"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.has {
		return ErrNotFound
	}
	f.value, f.has = "", false
	return nil
}

// ManualClock is a Clock for tests. A timer fires only when the test calls Fire.
type ManualClock struct{ timers chan chan time.Time }

func NewManualClock() *ManualClock { return &ManualClock{timers: make(chan chan time.Time, 8)} }

func (c *ManualClock) After(time.Duration) <-chan time.Time {
	t := make(chan time.Time, 1)
	c.timers <- t
	return t
}

// Fire fires the oldest timer. It waits until a call has set one.
func (c *ManualClock) Fire() { (<-c.timers) <- time.Time{} }

// WaitTimer waits until a call has set a timer, and leaves the timer unfired.
func (c *ManualClock) WaitTimer() { <-c.timers }
