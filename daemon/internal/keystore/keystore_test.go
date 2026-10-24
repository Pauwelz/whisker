package keystore_test

import (
	"errors"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/keystore"
)

func TestLoadCreatesAKeyOnce(t *testing.T) {
	s := keystore.New(keystore.NewFake(), keystore.NewManualClock())
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
	key, _, _ := keystore.New(fake, keystore.NewManualClock()).Load()
	stored, _ := fake.Get()
	if len(stored) != 44 || stored == string(key) {
		t.Errorf("stored value %q is not the base64 of a 32-byte key", stored)
	}
}

func TestMissingKeyringIsUnavailable(t *testing.T) {
	fake := keystore.NewFake()
	fake.Fail(errors.New("no secret service"))
	_, _, err := keystore.New(fake, keystore.NewManualClock()).Load()
	if !errors.Is(err, keystore.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestLockedKeyringTimesOutAndRetryJoinsTheAttempt(t *testing.T) {
	fake := keystore.NewFake()
	fake.Lock()
	clock := keystore.NewManualClock()
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
	s := keystore.New(fake, keystore.NewManualClock())
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
