package attention

import "testing"

func TestAttended(t *testing.T) {
	var a Attention
	if a.Open() || a.Attended() != "" {
		t.Fatal("a new Attention must be closed")
	}
	a.Set(true, "1@s.whatsapp.net")
	if !a.Open() || a.Attended() != "1@s.whatsapp.net" {
		t.Errorf("open on a Chat: Open=%v Attended=%q", a.Open(), a.Attended())
	}
	a.Set(true, "")
	if !a.Open() || a.Attended() != "" {
		t.Errorf("open on the Chat list: Open=%v Attended=%q", a.Open(), a.Attended())
	}
	a.Set(false, "1@s.whatsapp.net")
	if a.Open() || a.Attended() != "" {
		t.Errorf("closed: Open=%v Attended=%q", a.Open(), a.Attended())
	}
}
