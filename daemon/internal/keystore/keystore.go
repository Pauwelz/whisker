// Package keystore keeps the store key of the Profile in the Secret Service.
// go-keyring has no timeout, so every call here has its own (see docs/research/go-keyring.md).
package keystore

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Timeout is how long one call waits for the keyring.
const Timeout = 30 * time.Second

const keyBytes = 32

var (
	// ErrNotFound is the only "no key yet" signal from a Backend.
	ErrNotFound = errors.New("key not found")
	// ErrUnavailable means the keyring failed or did not answer in time.
	ErrUnavailable = errors.New("keyring unavailable")
)

// Backend is the keyring. Get and Delete return ErrNotFound when the key is absent.
type Backend interface {
	Get() (string, error)
	Set(value string) error
	Delete() error
}

// Clock lets tests control the timeout.
type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// RealClock is the wall clock.
var RealClock Clock = realClock{}

type attempt struct {
	done    chan struct{}
	key     []byte
	created bool
	err     error
}

// Store loads and deletes the store key.
type Store struct {
	backend Backend
	clock   Clock

	mu       sync.Mutex
	inflight *attempt
}

func New(backend Backend, clock Clock) *Store { return &Store{backend: backend, clock: clock} }

// Load returns the store key. It makes the key on first use and then reports created.
// One attempt runs at a time. After a timeout the attempt keeps running, and the next Load joins it.
func (s *Store) Load() (key []byte, created bool, err error) {
	s.mu.Lock()
	a := s.inflight
	if a == nil {
		a = &attempt{done: make(chan struct{})}
		s.inflight = a
		go s.run(a)
	}
	s.mu.Unlock()

	select {
	case <-a.done:
		return a.key, a.created, a.err
	case <-s.clock.After(Timeout):
		return nil, false, ErrUnavailable
	}
}

// run always calls Get before Set: a timed-out Set may have stored a key that nobody has seen.
func (s *Store) run(a *attempt) {
	a.key, a.created, a.err = s.getOrCreate()
	s.mu.Lock()
	s.inflight = nil
	s.mu.Unlock()
	close(a.done)
}

func (s *Store) getOrCreate() ([]byte, bool, error) {
	stored, err := s.backend.Get()
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(stored)
		if decodeErr != nil || len(key) != keyBytes {
			return nil, false, ErrUnavailable
		}
		return key, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, ErrUnavailable
	}
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	if err := s.backend.Set(base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, false, ErrUnavailable
	}
	return key, true, nil
}

// Delete removes the key. A missing key is not an error.
func (s *Store) Delete() error {
	result := make(chan error, 1)
	go func() { result <- s.backend.Delete() }()
	select {
	case err := <-result:
		if err == nil || errors.Is(err, ErrNotFound) {
			return nil
		}
		return ErrUnavailable
	case <-s.clock.After(Timeout):
		return ErrUnavailable
	}
}
