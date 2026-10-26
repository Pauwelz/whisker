package desktop

import "sync"

// Fake records the notifications, for tests.
type Fake struct {
	mu    sync.Mutex
	calls []Notification
}

// Notify records n.
func (f *Fake) Notify(n Notification) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, n)
}

// Calls returns the notifications so far.
func (f *Fake) Calls() []Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification{}, f.calls...)
}
