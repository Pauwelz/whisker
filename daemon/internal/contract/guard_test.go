package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGuardAnswersAPanickedRequestAndKeepsGoing(t *testing.T) {
	var sent []map[string]any
	var log bytes.Buffer
	s := &session{logOut: &log, send: func(f map[string]any) bool { sent = append(sent, f); return true }}
	s.current = json.RawMessage(`7`)
	if code := s.guard(func() int { panic("secret +31612345678") }); code != keepGoing {
		t.Fatalf("code = %d", code)
	}
	if len(sent) != 1 || sent[0]["error"].(map[string]any)["code"] != errInternal {
		t.Fatalf("reply = %v", sent)
	}
	if strings.Contains(log.String(), "secret") || s.current != nil {
		t.Errorf("log %q or current %s leaks the step", log.String(), s.current)
	}
	if code := s.guard(func() int { panic("event") }); code != keepGoing || len(sent) != 1 {
		t.Errorf("a panic with no request must send nothing: code %d, sent %d", code, len(sent))
	}
}
