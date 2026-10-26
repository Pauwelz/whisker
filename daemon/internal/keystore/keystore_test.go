package keystore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Pauwelz/whisker/daemon/internal/clock"
	"github.com/Pauwelz/whisker/daemon/internal/keystore"
)

func TestLoadCreatesAKeyOnce(t *testing.T) {
	s := keystore.New(keystore.NewFake(), clock.NewManual())
	key, created, err := s.Load()
	if err != nil || !created || len(key) != 32 {
		t.Fatalf("first Load = %d bytes, created %v, err %v", len(key), created, err)
	}
	again, created, err := s.Load()
	if err != nil || created || string(again) != string(key) {
		t.Fatalf("second Load must return the same key, created %v, err %v", created, err)
	}
}

func TestLoadStoresBase64(t *testing.T) {
	fake := keystore.NewFake()
	key, _, _ := keystore.New(fake, clock.NewManual()).Load()
	stored, _ := fake.Get()
	if len(stored) != 44 || stored == string(key) {
		t.Errorf("stored value %q is not the base64 of a 32-byte key", stored)
	}
}

func TestMissingKeyringIsUnavailable(t *testing.T) {
	fake := keystore.NewFake()
	fake.Fail(errors.New("no secret service"))
	_, _, err := keystore.New(fake, clock.NewManual()).Load()
	if !errors.Is(err, keystore.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestLockedKeyringTimesOutAndRetryJoinsTheAttempt(t *testing.T) {
	fake := keystore.NewFake()
	fake.Lock()
	clock := clock.NewManual()
	s := keystore.New(fake, clock)

	first := make(chan error, 1)
	go func() { _, _, err := s.Load(); first <- err }()
	clock.Fire()
	if err := <-first; !errors.Is(err, keystore.ErrUnavailable) {
		t.Fatalf("first Load err = %v, want ErrUnavailable", err)
	}

	retry := make(chan error, 1)
	go func() { _, _, err := s.Load(); retry <- err }() // Retry
	clock.WaitTimer()                                   // Retry has joined the attempt and waits
	fake.Unlock()                                       // the user answers the prompt
	if err := <-retry; err != nil {
		t.Fatalf("Retry err = %v", err)
	}
	if got := fake.Calls(); got != "get,set" {
		t.Errorf("keyring calls = %q, want one attempt: get,set", got)
	}
}

func TestDeleteRemovesTheKeyAndAcceptsAMissingKey(t *testing.T) {
	fake := keystore.NewFake()
	s := keystore.New(fake, clock.NewManual())
	if err := s.Delete(); err != nil {
		t.Fatalf("Delete without key: %v", err)
	}
	if _, _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Get(); !errors.Is(err, keystore.ErrNotFound) {
		t.Errorf("key still stored: %v", err)
	}
}

// A Delete that timed out still waits in the keyring. A Load after it must not reach the keyring first,
// or the late Delete removes the key of the new Profile.
func TestLoadWaitsForATimedOutDelete(t *testing.T) {
	fake := keystore.NewFake()
	fake.Lock()
	clock := clock.NewManual()
	s := keystore.New(fake, clock)

	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete() }()
	for fake.Calls() != "delete" { // the Delete is blocked in the keyring
		time.Sleep(time.Millisecond)
	}
	clock.Fire()
	if err := <-deleted; !errors.Is(err, keystore.ErrUnavailable) {
		t.Fatalf("Delete err = %v, want ErrUnavailable", err)
	}

	loaded := make(chan error, 1)
	go func() { _, _, err := s.Load(); loaded <- err }()
	clock.WaitTimer() // Load waits
	if got := fake.Calls(); got != "delete" {
		t.Errorf("keyring calls = %q, want only the waiting delete", got)
	}
	fake.Unlock()
	if err := <-loaded; err != nil {
		t.Fatalf("Load err = %v", err)
	}
	if got := fake.Calls(); got != "delete,get,set" {
		t.Errorf("keyring calls = %q, want delete,get,set", got)
	}
}

func TestReplaceWritesANewKeyOverTheItem(t *testing.T) {
	fake := keystore.NewFake()
	s := keystore.New(fake, clock.NewManual())
	old, _, _ := s.Load()
	key, err := s.Replace()
	if err != nil || len(key) != 32 || string(key) == string(old) {
		t.Fatalf("Replace = %d bytes, same as old %v, err %v", len(key), string(key) == string(old), err)
	}
	again, created, err := s.Load()
	if err != nil || created || string(again) != string(key) {
		t.Errorf("Load after Replace: created %v, same key %v, err %v", created, string(again) == string(key), err)
	}
}
