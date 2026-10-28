package desktop

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestHintsRunOpenChatWithTheTokenAsOneWord(t *testing.T) {
	h, err := hints(Notification{Summary: "Crew", Body: "491234@g.us", Token: `a b"c`})
	if err != nil {
		t.Fatal(err)
	}
	click := h[hintClick]
	if got, want := click.Value(), `["omarchy-shell","pauwelz.whisker","openChat","a b\"c"]`; got != want {
		t.Errorf("click hint = %v, want %v", got, want)
	}
	if s := click.Value().(string); strings.ContainsAny(s, "@") || strings.ContainsAny(s, "0123456789") {
		t.Errorf("click hint %q holds an @ or a digit of the Chat id", s)
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

// A hung bus fails each call at the limit and starts one connect only. A later call connects again once the hung one ended.
func TestAHungSessionBusFailsAtTheLimitWithOneConnect(t *testing.T) {
	release := make(chan struct{})
	var connects atomic.Int32
	b := &sessionBus{limit: 20 * time.Millisecond, connect: func() (*dbus.Conn, error) {
		connects.Add(1)
		<-release
		return &dbus.Conn{}, nil
	}}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.get(); err != errBusTimeout {
				t.Errorf("err = %v, want the timeout", err)
			}
		}()
	}
	wg.Wait()
	if n := connects.Load(); n != 1 {
		t.Errorf("connects = %d, want 1", n)
	}
	close(release)
	if conn, err := b.get(); err != nil || conn == nil {
		t.Errorf("get after the bus answered = %v, %v", conn, err)
	}
}
