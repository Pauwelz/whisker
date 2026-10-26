package desktop

import "testing"

func TestHintsRunOpenChatWithTheChatAsOneWord(t *testing.T) {
	h, err := hints(Notification{Chat: `a b"@g.us`})
	if err != nil {
		t.Fatal(err)
	}
	click := h[hintClick]
	if got, want := click.Value(), `["omarchy-shell","pauwelz.whisker","openChat","a b\"@g.us"]`; got != want {
		t.Errorf("click hint = %v, want %v", got, want)
	}
	if got := click.Signature().String(); got != "s" {
		t.Errorf("click signature = %q, want s", got)
	}
	if got := h[hintUrgency].Signature().String(); got != "y" {
		t.Errorf("urgency signature = %q, want y", got)
	}
}
