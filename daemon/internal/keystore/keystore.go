// Package keystore keeps the store key of the Profile in the Secret Service.
// go-keyring has no timeout, so every call here has its own (see docs/research/go-keyring.md).
package keystore

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
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

type attempt struct {
	done    chan struct{}
	key     []byte
	created bool
	err     error
}

// Store loads and deletes the store key.
type Store struct {
	backend Backend
	clock   clock.Clock

	mu       sync.Mutex
	inflight *attempt
	busy     sync.Mutex // one keyring call at a time: a timed-out Delete must finish before the next call starts
}

func New(backend Backend, clock clock.Clock) *Store { return &Store{backend: backend, clock: clock} }

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
	s.busy.Lock()
	a.key, a.created, a.err = s.getOrCreate()
	s.busy.Unlock()
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
	key, err := s.set()
	return key, err == nil, err
}

// set makes a key and stores it over any item.
func (s *Store) set() ([]byte, error) {
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := s.backend.Set(base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, ErrUnavailable
	}
	return key, nil
}

// exclusive runs fn after every earlier keyring call has ended. It stops waiting after Timeout, but fn still runs.
func (s *Store) exclusive(fn func() error) error {
	result := make(chan error, 1)
	go func() {
		s.busy.Lock()
		defer s.busy.Unlock()
		result <- fn()
	}()
	select {
	case err := <-result:
		return err
	case <-s.clock.After(Timeout):
		return ErrUnavailable
	}
}

// Delete removes the key. A missing key is not an error.
// It waits for earlier keyring calls, so a timed-out Delete cannot remove the key of a Profile that linked later.
func (s *Store) Delete() error {
	return s.exclusive(func() error {
		if err := s.backend.Delete(); err != nil && !errors.Is(err, ErrNotFound) {
			return ErrUnavailable
		}
		return nil
	})
}
