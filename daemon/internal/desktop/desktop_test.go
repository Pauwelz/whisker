package desktop

import (
	"strings"
	"testing"
)

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

func TestNeutraliseBreaksEveryMarkupTrigger(t *testing.T) {
	inputs := []string{
		`<a href="https://evil.example">bank.com</a>`,
		`&lt;a href="https://evil.example"&gt;bank.com&lt;/a&gt;`,
		`&amp;lt;b&amp;gt;hi&amp;lt;/b&amp;gt;`,
		`&#60;b&#62;hi&#60;/b&#62;`,
		`<b>hi</b>`,
		`see https://x.example`,
	}
	for _, in := range inputs {
		out := neutralise(in)
		if strings.Contains(out, "://") {
			t.Errorf("neutralise(%q) = %q, has ://", in, out)
		}
		for i := 0; i < len(out)-1; i++ {
			if (out[i] == '<' || out[i] == '&') && !strings.HasPrefix(out[i+1:], "\u200b") {
				t.Errorf("neutralise(%q) = %q, has a bare %c", in, out, out[i])
			}
		}
	}
	if got := neutralise("see you at 5"); got != "see you at 5" {
		t.Errorf("plain text changed: %q", got)
	}
}
